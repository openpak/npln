package npln

import (
	"context"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	authpb "openpak/stardew-valley/proto/auth/v1"
)

type authServer struct{ authpb.UnimplementedAuthServer }

// IssuePrearrangedUserToken is Stardew's first RPC (confirmed 2026-09-01), sent on two connections.
func (s *authServer) IssuePrearrangedUserToken(ctx context.Context, req *authpb.IssuePrearrangedUserTokenRequest) (*authpb.IssuePrearrangedUserTokenResponse, error) {
	pid, userPath, err := gatedIdentity(req.GetExternalIdToken(), req.GetTenant())
	if err != nil {
		return nil, err
	}
	log.Printf("[Auth] IssuePrearrangedUserToken pid=%d user=%s", pid, userPath)
	return &authpb.IssuePrearrangedUserTokenResponse{
		User:  &authpb.User{Name: userPath, ShortId: int64(req.GetUserIndex())},
		Token: newToken(pid, userPath, req.GetTenant()),
	}, nil
}

func (s *authServer) IssueToken(ctx context.Context, req *authpb.IssueTokenRequest) (*authpb.IssueTokenResponse, error) {
	pid, userPath, err := gatedIdentity(req.GetExternalIdToken(), tenantFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	log.Printf("[Auth] IssueToken pid=%d user=%s", pid, userPath)
	return &authpb.IssueTokenResponse{Token: newToken(pid, userPath, tenantFromCtx(ctx))}, nil
}

func (s *authServer) IssueAnonymousUserToken(ctx context.Context, req *authpb.IssueAnonymousUserTokenRequest) (*authpb.IssueAnonymousUserTokenResponse, error) {
	pid, userPath, err := gatedIdentity(req.GetExternalIdToken(), req.GetTenant())
	if err != nil {
		return nil, err
	}
	return &authpb.IssueAnonymousUserTokenResponse{Token: newToken(pid, userPath, req.GetTenant())}, nil
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
	return &authpb.RefreshTokenResponse{Token: newToken(pid, userPath, tenantFromCtx(ctx))}, nil
}

// ValidateToken: the bearer's signature is checked by callerPID on every call; the account gate
// happened at issue time (the adapter only knows verified, linked accounts).
func (s *authServer) ValidateToken(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
