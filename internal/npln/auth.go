package npln

import (
	"context"
	"log"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	authpb "openpak/splatoon-3/proto/auth/v1"
)

// authServer answers nn.npln.auth.v1.Auth. Which of these RPCs Splatoon 3 actually calls, and in
// what order, is unmeasured — the whole surface is implemented because each handler is three
// lines and the alternative is guessing. The [RPC] log lines from a real run decide what stays.
type authServer struct{ authpb.UnimplementedAuthServer }

func (s *authServer) IssuePrearrangedUserToken(_ context.Context, req *authpb.IssuePrearrangedUserTokenRequest) (*authpb.IssuePrearrangedUserTokenResponse, error) {
	pid, userPath, err := gatedIdentity(req.GetExternalIdToken(), resolveTenant(req.GetTenant()))
	if err != nil {
		return nil, err
	}
	log.Printf("[Auth] IssuePrearrangedUserToken pid=%d user=%s", pid, userPath)
	return &authpb.IssuePrearrangedUserTokenResponse{
		User:  &authpb.User{Name: userPath, ShortId: int64(req.GetUserIndex())},
		Token: newToken(pid, userPath, resolveTenant(req.GetTenant())),
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

func (s *authServer) IssueAnonymousUserToken(_ context.Context, req *authpb.IssueAnonymousUserTokenRequest) (*authpb.IssueAnonymousUserTokenResponse, error) {
	pid, userPath, err := gatedIdentity(req.GetExternalIdToken(), resolveTenant(req.GetTenant()))
	if err != nil {
		return nil, err
	}
	return &authpb.IssueAnonymousUserTokenResponse{Token: newToken(pid, userPath, resolveTenant(req.GetTenant()))}, nil
}

// RefreshToken re-issues for an identity ALREADY proven (bearer or our own signed refresh
// token); it is never a way in.
func (s *authServer) RefreshToken(ctx context.Context, req *authpb.RefreshTokenRequest) (*authpb.RefreshTokenResponse, error) {
	pid, ok := callerPID(ctx)
	if !ok {
		pid, ok = pidFromRefresh(req.GetRefreshToken())
	}
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "refresh token without a proven identity")
	}
	log.Printf("[Auth] RefreshToken pid=%d", pid)
	return &authpb.RefreshTokenResponse{Token: newToken(pid, req.GetUser(), tenantFromCtx(ctx))}, nil
}

// ValidateToken: the bearer's signature is checked by callerPID wherever identity matters, and
// the account gate happened at issue time — nx-baas only projects verified, linked accounts.
func (s *authServer) ValidateToken(_ context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
