package npln

import (
	"log"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	friendspb "github.com/openpak/npln/proto/friends/v1"
)

// presenceServer is nn.npln.friends.v1.PresenceService, KeepAlive only. Dinkum opens the stream
// right after ActivateUser (boot, 2026-09-18) and got Unimplemented.
//
// ponytail: pings are answered and published state is dropped; nobody here calls
// SubscribePresences yet. Splatoon 3's presence.go tracks who is online, port it when a title
// asks for friends' presence.
type presenceServer struct {
	friendspb.UnimplementedPresenceServiceServer
}

// The interval/deadline the real service announces; the client pings on one and gives up after
// the other (as in Splatoon 3).
var presenceHeartbeat = &friendspb.Heartbeat{
	Interval: durationpb.New(30 * time.Second),
	Deadline: durationpb.New(50 * time.Second),
}

// KeepAlive opens with a heartbeat, then answers every ping: the client gives up when its own
// pings go unanswered, even if we heartbeat on a timer of our own.
//
// The opening heartbeat is what makes the presence client "connected". Dinkum opens the stream
// and sends nothing; without it, SetPresenceHosting fails and the game aborts right after its
// room code arrives (2026-09-18, 56 s of silence on the stream before the abort).
func (p *presenceServer) KeepAlive(stream friendspb.PresenceService_KeepAliveServer) error {
	if err := stream.Send(&friendspb.KeepAliveResponse{Heartbeat: presenceHeartbeat}); err != nil {
		return nil
	}
	for {
		req, err := stream.Recv()
		if err != nil {
			return nil
		}
		if up := req.GetUpdatePresence(); up != nil {
			log.Printf("[Presence] UpdatePresence: %d attribute(s)", len(up.GetPresence().GetAttributes()))
		}
		if err := stream.Send(&friendspb.KeepAliveResponse{Heartbeat: presenceHeartbeat}); err != nil {
			return nil
		}
	}
}
