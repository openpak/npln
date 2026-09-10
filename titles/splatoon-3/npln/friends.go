package npln

// friends — nn.npln.friends.v1.Friends, built from the live OpenPak friend graph.
//
// It must be dynamic. A server that answers with a fixed snapshot instead of the caller's own
// friends stalls the game's session setup: the block list never becomes ready, and a multiplayer
// room shows an empty roster with invitations and room codes disabled.

import (
	"log"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"context"

	friendspb "github.com/openpak/npln/proto/friends/v1"
)

// friendKeepAlive is the interval we announce on the friends stream, and we send well inside it.
const friendKeepAlive = 50 * time.Second

// chunkBytes bounds one SubscribeFriendUsers message. The whole friend graph in a single message
// is a measured crash: a ~27 KB response aborted the game's plaza parser, with the error naming
// SubscribeFriendUsers, while the real service answers a few hundred bytes. Splitting is safe
// because the client accumulates the messages it receives off a stream.
func chunkBytes() int {
	if n, err := strconv.Atoi(envOr("NPLN_FRIENDS_CHUNK_BYTES", "4096")); err == nil && n > 0 {
		return n
	}
	return 4096
}

type friendsServer struct {
	friendspb.UnimplementedFriendsServer
}

// ActivateUser is the client announcing itself. Nothing to do but say yes; the identity was
// already proven at auth.
func (s *friendsServer) ActivateUser(_ context.Context, req *friendspb.ActivateUserRequest) (*friendspb.ActivateUserResponse, error) {
	log.Printf("[Friends] ActivateUser %q", req.GetName())
	return &friendspb.ActivateUserResponse{}, nil
}

// ListBlockingUsers: OpenPak has no block graph yet, so nobody is blocked. The empty answer still
// has to arrive — the client waits on it before its session setup is ready.
func (s *friendsServer) ListBlockingUsers(_ context.Context, _ *friendspb.ListBlockingUsersRequest) (*friendspb.ListBlockingUsersResponse, error) {
	return &friendspb.ListBlockingUsersResponse{}, nil
}

func friendUser(tenant, myUID string, f Friend) *friendspb.FriendUser {
	return &friendspb.FriendUser{
		Name:       tenant + "/users/" + myUID + "/friendUsers/" + f.UserID(),
		FriendUser: tenant + "/users/" + f.UserID(),
		NsaId:      f.BaasUserID,
		// Presence in both directions, as it is between real friends. Left unset the client can
		// treat the friend as presence-less and never show them online.
		Relationship: &friendspb.FriendUser_Relationship{PresenceDeliverable: true, PresenceReceivable: true},
	}
}

// callerAccount resolves the caller to their OpenPak account via the bearer token.
func callerAccount(ctx context.Context) (*Account, bool) {
	pid, ok := callerPID(ctx)
	if !ok {
		return nil, false
	}
	acc, err := lookupAccount(pid)
	if err != nil {
		log.Printf("[Friends] account lookup for pid=%d: %v", pid, err)
		return nil, false
	}
	return acc, true
}

func (s *friendsServer) ListFriendUsers(ctx context.Context, _ *friendspb.ListFriendUsersRequest) (*friendspb.ListFriendUsersResponse, error) {
	out := &friendspb.ListFriendUsersResponse{}
	me, ok := callerAccount(ctx)
	if !ok {
		return out, nil
	}
	for _, f := range me.Friends {
		out.FriendUsers = append(out.FriendUsers, friendUser(tenantFromCtx(ctx), me.UserID(), f))
	}
	return out, nil
}

// friendAccounts builds the caller's friend list in the shape the stream carries.
func friendAccounts(ctx context.Context, me *Account) []*friendspb.SubscribeFriendUsersResponse_FriendAccount {
	tenant := tenantFromCtx(ctx)
	out := make([]*friendspb.SubscribeFriendUsersResponse_FriendAccount, 0, len(me.Friends))
	for _, f := range me.Friends {
		out = append(out, &friendspb.SubscribeFriendUsersResponse_FriendAccount{
			NsaId: f.BaasUserID,
			Users: []*friendspb.FriendUser{friendUser(tenant, me.UserID(), f)},
		})
	}
	return out
}

// chunkAccounts splits the list so no single message greatly exceeds the byte budget. An account
// that is on its own over budget still goes out alone rather than being dropped: a friend missing
// from the list is worse than a large message.
func chunkAccounts(accounts []*friendspb.SubscribeFriendUsersResponse_FriendAccount, budget int) [][]*friendspb.SubscribeFriendUsersResponse_FriendAccount {
	if len(accounts) == 0 {
		return nil
	}
	var out [][]*friendspb.SubscribeFriendUsersResponse_FriendAccount
	cur, size := []*friendspb.SubscribeFriendUsersResponse_FriendAccount{}, 0
	for _, a := range accounts {
		n := proto.Size(a)
		if len(cur) > 0 && size+n > budget {
			out = append(out, cur)
			cur, size = nil, 0
		}
		cur = append(cur, a)
		size += n
	}
	return append(out, cur)
}

// SubscribeFriendUsers streams the caller's friend graph, then keep-alives for the life of the
// session — the client reads a close as "friends lost".
func (s *friendsServer) SubscribeFriendUsers(_ *friendspb.SubscribeFriendUsersRequest, stream friendspb.Friends_SubscribeFriendUsersServer) error {
	ctx := stream.Context()
	ka := durationpb.New(friendKeepAlive)

	var accounts []*friendspb.SubscribeFriendUsersResponse_FriendAccount
	if me, ok := callerAccount(ctx); ok {
		accounts = friendAccounts(ctx, me)
		log.Printf("[Friends] Subscribe uid=%s -> %d friend(s)", me.UserID(), len(accounts))
	}
	if len(accounts) == 0 {
		// A friendless response is a known risk, not a known-good answer: an empty
		// SubscribeFriendUsers has been measured aborting the game's plaza resource-path parser
		// (2162-0001). We have nothing truthful to put here instead — inventing a friend would be
		// worse — so it goes out empty and loudly. See docs/evidence-needed.md.
		log.Printf("[Friends] Subscribe -> NO friends; an empty list has been measured aborting the plaza")
	}

	// The first message carries the keep-alive contract the client times itself against.
	batches := chunkAccounts(accounts, chunkBytes())
	if len(batches) == 0 {
		batches = [][]*friendspb.SubscribeFriendUsersResponse_FriendAccount{nil}
	}
	for i, b := range batches {
		msg := &friendspb.SubscribeFriendUsersResponse{FriendAccounts: b}
		if i == 0 {
			msg.KeepAliveInterval = ka
		}
		if err := stream.Send(msg); err != nil {
			return nil
		}
	}

	t := time.NewTicker(friendKeepAlive / 2) // comfortably inside the announced interval
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
