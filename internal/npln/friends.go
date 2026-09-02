package npln

import (
	"context"
	"log"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	friendspb "github.com/NextendoNetwork/stardew-nextendo/proto/friends/v1"
)

type friendsServer struct {
	friendspb.UnimplementedFriendsServer
}

func (s *friendsServer) ActivateUser(ctx context.Context, req *friendspb.ActivateUserRequest) (*friendspb.ActivateUserResponse, error) {
	log.Printf("[Friends] ActivateUser %q", req.GetName())
	return &friendspb.ActivateUserResponse{}, nil
}

func (s *friendsServer) ListBlockingUsers(ctx context.Context, req *friendspb.ListBlockingUsersRequest) (*friendspb.ListBlockingUsersResponse, error) {
	return &friendspb.ListBlockingUsersResponse{}, nil
}

func friendUser(tenant, myUID string, f Friend) *friendspb.FriendUser {
	return &friendspb.FriendUser{
		Name:       tenant + "/users/" + myUID + "/friendUsers/" + f.UserID,
		FriendUser: tenant + "/users/" + f.UserID,
		NsaId:      f.AccountHex,
		// Both directions of presence are on, as between real friends; left empty the client may
		// treat the friend as presence-less (measurement, 2026-09-01).
		Relationship: &friendspb.FriendUser_Relationship{PresenceDeliverable: true, PresenceReceivable: true},
	}
}

func (s *friendsServer) ListFriendUsers(ctx context.Context, req *friendspb.ListFriendUsersRequest) (*friendspb.ListFriendUsersResponse, error) {
	out := &friendspb.ListFriendUsersResponse{}
	pid, ok := callerPID(ctx)
	if !ok {
		return out, nil
	}
	me, err := lookupAccount(pid)
	if err != nil {
		return out, nil
	}
	for _, f := range me.Friends {
		out.FriendUsers = append(out.FriendUsers, friendUser(tenantFromCtx(ctx), me.UserID, f))
	}
	return out, nil
}

// SubscribeFriendUsers streams the Nextendo friend graph once, then keep-alives for the session's
// life (the client treats a close as "friends lost"). Shape: FriendAccounts[{nsa_id, users}].
func (s *friendsServer) SubscribeFriendUsers(req *friendspb.SubscribeFriendUsersRequest, stream friendspb.Friends_SubscribeFriendUsersServer) error {
	ctx := stream.Context()
	ka := durationpb.New(50 * time.Second)
	first := &friendspb.SubscribeFriendUsersResponse{KeepAliveInterval: ka}
	if pid, ok := callerPID(ctx); ok {
		if me, err := lookupAccount(pid); err == nil {
			for _, f := range me.Friends {
				first.FriendAccounts = append(first.FriendAccounts, &friendspb.SubscribeFriendUsersResponse_FriendAccount{
					NsaId: f.AccountHex,
					Users: []*friendspb.FriendUser{friendUser(tenantFromCtx(ctx), me.UserID, f)},
				})
			}
			log.Printf("[Friends] Subscribe pid=%d -> %d friend(s)", pid, len(me.Friends))
		}
	}
	if err := stream.Send(first); err != nil {
		return err
	}
	t := time.NewTicker(20 * time.Second) // well inside the announced 50 s
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := stream.Send(&friendspb.SubscribeFriendUsersResponse{KeepAliveInterval: ka}); err != nil {
				return nil
			}
		}
	}
}
