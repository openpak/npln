// Package bans locks a banned OpenPak account out of every NPLN title
// (website/docs/ban-lookup.md is the contract).
//
// nx-baas is where every title already proves a player, and it answers
// /internal/switch/identity with 403 {"error":"account_banned"} (or
// "account_deleted") for a locked-out account. This package asks it again by
// pid: at every token issue, on every RPC that carries one of our tokens, and
// every 30 s on a stream that stays open, so a ban ends a live session within
// a minute (nx-baas caches an active answer for at most 30 s, and so does this
// package).
//
// The refusal is gRPC PermissionDenied with a message saying the account is
// banned, the code NPLN answers an authenticated caller it will not serve.
package bans

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrBanned is nx-baas refusing the account: banned, or being deleted.
var ErrBanned = errors.New("account banned")

// Message is what a banned caller is told.
const Message = "This OpenPak account has been banned"

const (
	activeTTL  = 30 * time.Second // how long an "active" answer is trusted; "banned" is never cached
	sweepEvery = 30 * time.Second // how often an open stream is re-checked
)

// Refused is the gRPC error a banned caller gets.
func Refused() error { return status.Error(codes.PermissionDenied, Message) }

// IsRefused reports whether err is Refused().
func IsRefused(err error) bool {
	s, ok := status.FromError(err)
	return ok && s.Code() == codes.PermissionDenied && s.Message() == Message
}

// Gate asks nx-baas whether a pid's account may play.
type Gate struct {
	url, key string
	client   *http.Client
	now      func() time.Time
	every    time.Duration

	mu     sync.Mutex
	active map[uint64]time.Time // pid -> active answer expiry
	users  map[string]uint64    // NPLN user id -> pid, filled at token issue
}

// New builds a gate against nx-baas's internal API.
func New(url, key string) *Gate {
	return &Gate{
		url: strings.TrimRight(url, "/"), key: key,
		client: &http.Client{Timeout: 5 * time.Second},
		now:    time.Now, every: sweepEvery,
		active: map[uint64]time.Time{}, users: map[string]uint64{},
	}
}

// Default reads NX_INTERNAL_URL and NX_INTERNAL_KEY, the variables every title
// already uses to reach nx-baas.
var Default = New(envOr("NX_INTERNAL_URL", "http://127.0.0.1:20070"), os.Getenv("NX_INTERNAL_KEY"))

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// Check returns nil when the account may play, ErrBanned when nx-baas refuses
// it, and any other error when nx-baas could not answer.
func (g *Gate) Check(pid uint64) error {
	g.mu.Lock()
	if exp, ok := g.active[pid]; ok && g.now().Before(exp) {
		g.mu.Unlock()
		return nil
	}
	g.mu.Unlock()

	body, _ := json.Marshal(map[string]any{"pid": pid})
	req, err := http.NewRequest(http.MethodPost, g.url+"/internal/switch/identity", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", g.key)
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("ban check: %w", err)
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		g.mu.Lock()
		g.active[pid] = g.now().Add(activeTTL)
		g.mu.Unlock()
		return nil
	case http.StatusForbidden:
		g.mu.Lock()
		delete(g.active, pid)
		g.mu.Unlock()
		return ErrBanned
	default:
		return fmt.Errorf("ban check: %s", resp.Status)
	}
}

// Remember records which pid an NPLN user id belongs to, so a call that only
// names the user (a gamesync session token) can still be checked.
func (g *Gate) Remember(uid string, pid uint64) {
	if uid == "" || pid == 0 {
		return
	}
	g.mu.Lock()
	g.users[uid] = pid
	g.mu.Unlock()
}

// PID is the pid Remember recorded for uid.
func (g *Gate) PID(uid string) (uint64, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	pid, ok := g.users[uid]
	return pid, ok
}

// Subject names the caller of an RPC: its pid, or its NPLN user id when the
// token carries no pid. A title supplies it from its own verified token.
type Subject func(ctx context.Context) (pid uint64, uid string)

func (g *Gate) pidOf(ctx context.Context, subject Subject) uint64 {
	pid, uid := subject(ctx)
	if pid == 0 && uid != "" {
		pid, _ = g.PID(uid)
	}
	return pid
}

// banned is true only for a positive "banned": mid-session, an nx-baas outage
// keeps the player (contract rule 4).
func (g *Gate) banned(pid uint64) bool {
	return pid != 0 && errors.Is(g.Check(pid), ErrBanned)
}

// Unary refuses any call whose token belongs to a banned account.
func (g *Gate) Unary(subject Subject) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		if g.banned(g.pidOf(ctx, subject)) {
			return nil, Refused()
		}
		return h(ctx, req)
	}
}

// Stream refuses a stream opened by a banned account, and ends an open one
// whose account is banned while it runs. Returning from the interceptor
// finishes the RPC: gRPC cancels the stream's context, so the handler's next
// Recv or Send fails and its own cleanup runs.
func (g *Gate) Stream(subject Subject) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		pid := g.pidOf(ss.Context(), subject)
		if g.banned(pid) {
			return Refused()
		}
		if pid == 0 {
			return h(srv, ss)
		}
		done := make(chan error, 1)
		go func() { done <- h(srv, ss) }()
		tick := time.NewTicker(g.every)
		defer tick.Stop()
		for {
			select {
			case err := <-done:
				return err
			case <-tick.C:
				if g.banned(pid) {
					return Refused()
				}
			}
		}
	}
}
