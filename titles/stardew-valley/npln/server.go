// Package npln is the Stardew Valley NPLN service: nn.npln.* over gRPC/HTTP2/TLS, the control
// plane the Switch client talks to for identity, friends and farm sessions. Independently
// written; the transport quirks below were measured on the family's reference NPLN server.
package npln

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/openpak/npln/rpclog"
	"log"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"

	authpb "github.com/openpak/npln/proto/auth/v1"
	friendspb "github.com/openpak/npln/proto/friends/v1"
	gspb "github.com/openpak/npln/proto/gamesync/v1"
	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
)

// Tenant is the hosted title's NPLN tenant (observed in every RPC's npln-tenant-id).
// Stardew Valley's by default; NewServer sets it for a title that reuses this service set.
//
// ponytail: a package variable, because the binary hosts one title per process (see
// cmd/nplnd). Make it a Server field the day two tenants share a process.
var Tenant = StardewTenant

// StardewTenant is Stardew Valley's tenant.
const StardewTenant = "tenants/t-9f607adf-lp1"

// AppID is Stardew Valley's title id; each title passes its own to NewServer for the access
// token's npln.app_id claim.
const AppID = "0100e65002bb8000"

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envInt(k string, d int32) int32 {
	if n, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return int32(n)
	}
	return d
}

func uuid4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func lastSeg(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func mdGet(ctx context.Context, k string) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(k); len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// tenantFromCtx: resource names must live under the CALLER's tenant (the client rejects foreign ones).
func tenantFromCtx(ctx context.Context) string {
	if t := mdGet(ctx, "npln-tenant-id"); t != "" {
		return "tenants/" + t
	}
	return Tenant
}

// uidFromCtx: the caller's user id from the `uid` metadata ("u-…"), resolves "users/current".
func uidFromCtx(ctx context.Context) string { return mdGet(ctx, "uid") }

func short(s string) string {
	if len(s) > 24 {
		return s[:24] + "…"
	}
	return s
}

// Nintendo's gateway stamps npln-grpc-type on every response; the client is used to reading it.
func typeUnary(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	_ = grpc.SetHeader(ctx, metadata.Pairs("npln-grpc-type", "Unary"))
	provenByBearer(ctx)
	auth := mdGet(ctx, "authorization")
	log.Printf("[RPC] %s tenant=%q uid=%q auth=%q", info.FullMethod, mdGet(ctx, "npln-tenant-id"), uidFromCtx(ctx), short(auth))
	resp, err := h(ctx, req)
	if err != nil {
		log.Printf("[RPC] %s -> ERROR %v", info.FullMethod, err)
	}
	return resp, err
}

func typeStream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
	kind := "ServerStreaming"
	switch {
	case info.IsClientStream && info.IsServerStream:
		kind = "BidirectionalStreaming"
	case info.IsClientStream:
		kind = "ClientStreaming"
	}
	_ = ss.SetHeader(metadata.Pairs("npln-grpc-type", kind))
	provenByBearer(ss.Context())
	log.Printf("[RPC] %s (%s) uid=%q", info.FullMethod, kind, uidFromCtx(ss.Context()))
	err := h(srv, ss)
	if err != nil {
		log.Printf("[RPC] %s -> ERROR %v", info.FullMethod, err)
	}
	return err
}

// unknownService logs any method we do not serve yet: that log line IS the next work item.
func unknownService(_ any, ss grpc.ServerStream) error {
	rpclog.Unimplemented(ss)
	m, _ := grpc.MethodFromServerStream(ss)
	log.Printf("[RPC] UNIMPLEMENTED %s uid=%q — next handler to write", m, uidFromCtx(ss.Context()))
	return grpc.Errorf(12, "method %s not implemented", m) //nolint:staticcheck // codes.Unimplemented
}

// connTracer logs connection lifetime: "h2 established" with zero RPCs is the client-side cancel signature.
type connTracer struct{ open atomic.Int64 }

func (*connTracer) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context { return ctx }
func (*connTracer) HandleRPC(context.Context, stats.RPCStats)                       {}

// TagConn gives every connection its own record of the identity it has proven (auth.go).
func (*connTracer) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return context.WithValue(ctx, connProofKey{}, &connProof{})
}
func (c *connTracer) HandleConn(_ context.Context, s stats.ConnStats) {
	switch s.(type) {
	case *stats.ConnBegin:
		log.Printf("[CONN] begin (h2 established) open=%d", c.open.Add(1))
	case *stats.ConnEnd:
		log.Printf("[CONN] end open=%d", c.open.Add(-1))
	}
}

// NewServer wires every service. creds==nil gives plaintext h2c (behind a TLS-terminating edge).
// tenant is the title's NPLN tenant ("tenants/t-…-lp1"); "" keeps Stardew Valley's.
func NewServer(creds credentials.TransportCredentials, tenant, appID string) *grpc.Server {
	if tenant != "" {
		Tenant = tenant
	}
	startRegions()
	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(typeUnary, rpclog.Unary),
		grpc.ChainStreamInterceptor(typeStream, rpclog.Stream),
		grpc.UnknownServiceHandler(unknownService),
		grpc.StatsHandler(&connTracer{}),
		// The client pings often, also without streams; grpc-go's default policy answers with
		// GOAWAY(ENHANCE_YOUR_CALM) and kills every in-flight RPC (measured on the reference server).
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 5 * time.Second, PermitWithoutStream: true}),
		// Probe the console ourselves: a client that vanishes without closing (power loss,
		// emulator killed) keeps its KeepUserSession stream — and its farm seat — until the
		// connection is declared dead. 15 s idle ping + 10 s timeout = gone within ~25 s.
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: 15 * time.Second, Timeout: 10 * time.Second}),
	}
	if creds != nil {
		opts = append(opts, grpc.Creds(creds))
	}
	s := grpc.NewServer(opts...)
	authpb.RegisterAuthServer(s, &authServer{appID: appID})
	friendspb.RegisterFriendsServer(s, &friendsServer{})
	friendspb.RegisterPresenceServiceServer(s, &presenceServer{})
	mm := newSessionServer()
	mmpb.RegisterGameSessionServiceServer(s, mm)
	gspb.RegisterGamesyncServer(s, newGamesync(mm)) // session transport: same listener (host:port points here)
	return s
}
