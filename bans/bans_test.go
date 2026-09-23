package bans

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
)

type fakeBaas struct {
	*httptest.Server
	status atomic.Int32
	calls  atomic.Int32
}

func newFake(t *testing.T) *fakeBaas {
	f := &fakeBaas{}
	f.status.Store(http.StatusOK)
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.URL.Path != "/internal/switch/identity" || r.Header.Get("X-Internal-Key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(int(f.status.Load()))
	}))
	t.Cleanup(f.Close)
	return f
}

func TestCheckCachesOnlyActive(t *testing.T) {
	f := newFake(t)
	g := New(f.URL, "k")
	now := time.Now()
	g.now = func() time.Time { return now }

	if err := g.Check(7); err != nil {
		t.Fatal(err)
	}
	f.status.Store(http.StatusForbidden)
	if err := g.Check(7); err != nil {
		t.Fatal("an active answer is trusted for its TTL")
	}
	now = now.Add(activeTTL + time.Second)
	if err := g.Check(7); !errors.Is(err, ErrBanned) {
		t.Fatalf("after the TTL: want ErrBanned, got %v", err)
	}
	calls := f.calls.Load()
	g.Check(7)
	if f.calls.Load() == calls {
		t.Fatal("a banned answer must not be cached: an unban has to work within the minute")
	}

	f.status.Store(http.StatusBadGateway)
	if err := g.Check(8); err == nil || errors.Is(err, ErrBanned) {
		t.Fatalf("outage: want a plain error, got %v", err)
	}
}

func TestUnaryRefusesBanned(t *testing.T) {
	f := newFake(t)
	g := New(f.URL, "k")
	subject := func(context.Context) (uint64, string) { return 0, "u-abc" }
	g.Remember("u-abc", 9)
	h := func(context.Context, any) (any, error) { return "ok", nil }

	if resp, err := g.Unary(subject)(context.Background(), nil, &grpc.UnaryServerInfo{}, h); err != nil || resp != "ok" {
		t.Fatalf("active: %v %v", resp, err)
	}
	f.status.Store(http.StatusForbidden)
	g.active = map[uint64]time.Time{}
	if _, err := g.Unary(subject)(context.Background(), nil, &grpc.UnaryServerInfo{}, h); !IsRefused(err) {
		t.Fatalf("banned: want Refused, got %v", err)
	}
	f.status.Store(http.StatusBadGateway)
	g.active = map[uint64]time.Time{}
	if _, err := g.Unary(subject)(context.Background(), nil, &grpc.UnaryServerInfo{}, h); err != nil {
		t.Fatalf("outage mid-session must not refuse: %v", err)
	}
}

type fakeStream struct{ grpc.ServerStream }

func (fakeStream) Context() context.Context { return context.Background() }

func TestStreamEndsWhenBannedMidSession(t *testing.T) {
	f := newFake(t)
	g := New(f.URL, "k")
	g.every = 20 * time.Millisecond
	now := time.Now()
	g.now = func() time.Time { return now } // frozen clock: the cache is cleared by hand below
	subject := func(context.Context) (uint64, string) { return 5, "" }

	release := make(chan struct{})
	defer close(release)
	h := func(any, grpc.ServerStream) error { <-release; return nil }

	result := make(chan error, 1)
	go func() { result <- g.Stream(subject)(nil, fakeStream{}, &grpc.StreamServerInfo{}, h) }()

	time.Sleep(60 * time.Millisecond)
	select {
	case err := <-result:
		t.Fatalf("active stream ended: %v", err)
	default:
	}
	f.status.Store(http.StatusForbidden)
	g.mu.Lock()
	g.active = map[uint64]time.Time{}
	g.mu.Unlock()
	select {
	case err := <-result:
		if !IsRefused(err) {
			t.Fatalf("want Refused, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("banned player's stream stayed open")
	}
}
