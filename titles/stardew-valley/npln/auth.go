package npln

import (
	"context"
	"log"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	authpb "github.com/openpak/npln/proto/auth/v1"
)

// authServer issues tokens for one title: appID goes into every access token's npln.app_id,
// which the client compares with its own title before it creates a session (Dinkum refused
// to host, 2321-5760, with no call made, while the tokens named Stardew).
type authServer struct {
	authpb.UnimplementedAuthServer
	appID string
}

// IssuePrearrangedUserToken is Stardew's first RPC (confirmed 2026-09-01), sent on two connections.
func (s *authServer) IssuePrearrangedUserToken(ctx context.Context, req *authpb.IssuePrearrangedUserTokenRequest) (*authpb.IssuePrearrangedUserTokenResponse, error) {
	pid, userPath, err := gatedIdentity(req.GetExternalIdToken(), req.GetTenant())
	if err != nil {
		return nil, err
	}
	proven(ctx, pid, userPath)
	log.Printf("[Auth] IssuePrearrangedUserToken pid=%d user=%s", pid, userPath)
	return &authpb.IssuePrearrangedUserTokenResponse{
		User:  &authpb.User{Name: userPath, ShortId: int64(req.GetUserIndex())},
		Token: newToken(pid, userPath, req.GetTenant(), s.appID),
	}, nil
}

func (s *authServer) IssueToken(ctx context.Context, req *authpb.IssueTokenRequest) (*authpb.IssueTokenResponse, error) {
	pid, userPath, err := gatedIdentity(req.GetExternalIdToken(), tenantFromCtx(ctx))
	if err != nil {
		p, ok := ctx.Value(connProofKey{}).(*connProof)
		if !ok || req.GetUser() == "" || p.user() != req.GetUser() {
			return nil, err
		}
		// Dinkum re-issues when it hosts, with a 32-byte non-JWT in place of the id token
		// (2026-09-18), on the connection that proved this same user moments before. Nobody else
		// can be on that connection, so the earlier proof stands; any other user still fails.
		pid, userPath = p.pid, p.path
		log.Printf("[Auth] IssueToken: token not provable, user already proven on this connection -> re-issued")
	}
	proven(ctx, pid, userPath)
	log.Printf("[Auth] IssueToken pid=%d user=%s", pid, userPath)
	return &authpb.IssueTokenResponse{Token: newToken(pid, userPath, tenantFromCtx(ctx), s.appID)}, nil
}

func (s *authServer) IssueAnonymousUserToken(ctx context.Context, req *authpb.IssueAnonymousUserTokenRequest) (*authpb.IssueAnonymousUserTokenResponse, error) {
	pid, userPath, err := gatedIdentity(req.GetExternalIdToken(), req.GetTenant())
	if err != nil {
		return nil, err
	}
	return &authpb.IssueAnonymousUserTokenResponse{Token: newToken(pid, userPath, req.GetTenant(), s.appID)}, nil
}

// RefreshToken re-issues for an identity ALREADY proven (bearer or signed refresh token); never anonymous.
func (s *authServer) RefreshToken(ctx context.Context, req *authpb.RefreshTokenRequest) (*authpb.RefreshTokenResponse, error) {
	pid, ok := callerPID(ctx)
	if !ok {
		pid, ok = pidFromRefresh(req.GetRefreshToken())
	}
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "refresh token without a proven identity")
	}
	// The user the request names is not consulted: the new token is for whoever proved the PID.
	userPath, err := userPathForPID(ctx, pid)
	if err != nil {
		return nil, err
	}
	log.Printf("[Auth] RefreshToken pid=%d", pid)
	return &authpb.RefreshTokenResponse{Token: newToken(pid, userPath, tenantFromCtx(ctx), s.appID)}, nil
}

// ValidateToken: the bearer's signature is checked by callerPID on every call; the account gate
// happened at issue time (the adapter only knows verified, linked accounts).
func (s *authServer) ValidateToken(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

// connProof is what one connection has proven: the last identity an id token established on it.
type connProof struct {
	mu   sync.Mutex
	pid  uint64
	path string
}

type connProofKey struct{}

func (p *connProof) user() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.path
}

func proven(ctx context.Context, pid uint64, userPath string) {
	if p, ok := ctx.Value(connProofKey{}).(*connProof); ok {
		p.mu.Lock()
		p.pid, p.path = pid, userPath
		p.mu.Unlock()
	}
}
