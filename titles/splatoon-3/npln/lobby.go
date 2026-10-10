package npln

// lobby — nn.npln.toyohr.v1.LobbyMessaging, plus the two small boot-time services that sit next
// to it: toyohr.v1.UserScreening/GetViolation and maintenance.v1.MaintenanceScheduleService.
//
// LobbyMessaging is a per-player message feed. Each console holds RecvMessage open for its whole
// session; SendMessage delivers a message body to the named users, SendLobbyMessage to everyone
// listening on a lobby. Facts this follows (docs/provenance.md):
//
//   - The game sends its own "NplnLogin" message addressed to itself and leaves the loading
//     screen only when it comes back on RecvMessage.
//   - The stream opens with a KeepAlive carrying the stream's cursor, and every delivery carries a
//     strictly increasing cursor ({"v":1,"t":"<unix ms>-<n>"}); the client drops anything older
//     than the last cursor it saw.
//   - sender_user is the resolved user name "tenants/t-dce9377b-lp1/users/<id>", never the
//     "tenants/current" alias the client writes (the client copies it with a fixed length).
//   - The stream is held open with periodic KeepAlives.

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	mtpb "github.com/openpak/npln/proto/maintenance/v1"
	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
)

// The lobby stream's heartbeat, and the idle timeout it announces (three missed heartbeats).
const (
	lobbyHeartbeat   = 20 * time.Second
	lobbyIdleTimeout = 3 * lobbyHeartbeat
)

// ---- cursors ----

var cursor = struct {
	sync.Mutex
	ms  int64
	seq int
}{}

// nextCursor returns a resume token strictly greater than every one handed out before.
func nextCursor() string {
	cursor.Lock()
	defer cursor.Unlock()
	now := time.Now().UnixMilli()
	if now > cursor.ms {
		cursor.ms, cursor.seq = now, 0
	} else {
		cursor.seq++
	}
	return fmt.Sprintf(`{"v":1,"t":"%d-%d"}`, cursor.ms, cursor.seq)
}

// ---- who is listening ----

type lobbyListener struct {
	uid   string
	lobby string
	ch    chan *toyohrpb.RecvMessageResponse
}

var lobbies = struct {
	sync.Mutex
	next int
	all  map[int]*lobbyListener
}{all: map[int]*lobbyListener{}}

func listen(uid, lobby string) (int, *lobbyListener) {
	l := &lobbyListener{uid: uid, lobby: lobby, ch: make(chan *toyohrpb.RecvMessageResponse, 64)}
	lobbies.Lock()
	defer lobbies.Unlock()
	lobbies.next++
	lobbies.all[lobbies.next] = l
	return lobbies.next, l
}

func unlisten(id int) {
	lobbies.Lock()
	delete(lobbies.all, id)
	lobbies.Unlock()
}

// deliver hands body to every open stream match accepts, and reports how many took it. A player
// who is not listening misses the message, as a lobby notification would be missed.
func deliver(sender string, body *toyohrpb.MessageBody, match func(*lobbyListener) bool) int {
	msg := &toyohrpb.RecvMessageResponse{Payload: &toyohrpb.RecvMessageResponse_Message{Message: &toyohrpb.ReceivedMessage{
		MessageBody:        body,
		SenderUser:         Tenant + "/users/" + sender,
		MessageResumeToken: nextCursor(),
		SendTime:           timestamppb.Now(),
	}}}
	lobbies.Lock()
	defer lobbies.Unlock()
	n := 0
	for _, l := range lobbies.all {
		if !match(l) {
			continue
		}
		select {
		case l.ch <- msg:
			n++
		default:
			log.Printf("[Lobby] a listener's queue is full; message dropped for it")
		}
	}
	return n
}

type lobbyMessaging struct {
	toyohrpb.UnimplementedLobbyMessagingServer
}

func lobbyKeepAlive() *toyohrpb.RecvMessageResponse {
	return &toyohrpb.RecvMessageResponse{Payload: &toyohrpb.RecvMessageResponse_KeepAlive{KeepAlive: &toyohrpb.KeepAlive{
		IdleTimeout:        durationpb.New(lobbyIdleTimeout),
		MessageResumeToken: nextCursor(),
	}}}
}

func (s *lobbyMessaging) RecvMessage(req *toyohrpb.RecvMessageRequest, stream toyohrpb.LobbyMessaging_RecvMessageServer) error {
	uid := callerUID(stream.Context())
	if uid == "" {
		return status.Error(codes.Unauthenticated, "no bearer token for this call")
	}
	id, l := listen(uid, lastSeg(req.GetLobby()))
	defer unlisten(id)
	log.Printf("[Lobby] RecvMessage open (lobby set: %t, resume token: %t)", req.GetLobby() != "", req.GetMessageResumeToken() != "")
	if err := stream.Send(lobbyKeepAlive()); err != nil {
		return err
	}
	t := time.NewTicker(lobbyHeartbeat)
	defer t.Stop()
	for {
		select {
		case <-stream.Context().Done():
			log.Printf("[Lobby] RecvMessage closed by the client")
			return nil
		case m := <-l.ch:
			if err := stream.Send(m); err != nil {
				return err
			}
		case <-t.C:
			if err := stream.Send(lobbyKeepAlive()); err != nil {
				return err
			}
		}
	}
}

func (s *lobbyMessaging) SendMessage(ctx context.Context, req *toyohrpb.SendMessageRequest) (*emptypb.Empty, error) {
	uid := callerUID(ctx)
	if uid == "" {
		return nil, status.Error(codes.Unauthenticated, "no bearer token for this call")
	}
	to := map[string]bool{}
	self := false
	for _, r := range req.GetReceiverUsers() {
		id := lastSeg(r)
		to[id] = true
		self = self || id == uid
	}
	n := deliver(uid, req.GetMessageBody(), func(l *lobbyListener) bool { return to[l.uid] })
	log.Printf("[Lobby] SendMessage type=%s receivers=%d self=%t delivered=%d",
		msgType(req.GetMessageBody()), len(to), self, n)
	return &emptypb.Empty{}, nil
}

func (s *lobbyMessaging) SendLobbyMessage(ctx context.Context, req *toyohrpb.SendLobbyMessageRequest) (*emptypb.Empty, error) {
	uid := callerUID(ctx)
	if uid == "" {
		return nil, status.Error(codes.Unauthenticated, "no bearer token for this call")
	}
	lobby := lastSeg(req.GetLobby())
	n := deliver(uid, req.GetMessageBody(), func(l *lobbyListener) bool { return l.lobby == lobby })
	log.Printf("[Lobby] SendLobbyMessage type=%s delivered=%d", msgType(req.GetMessageBody()), n)
	return &emptypb.Empty{}, nil
}

// msgType is the message's type name for the log: a protocol constant ("NplnLogin"), never
// player data; anything that does not look like one is reduced to its length.
func msgType(b *toyohrpb.MessageBody) string {
	t := lastSeg(b.GetMessageType())
	if len(t) > 0 && len(t) <= 40 && strings.Trim(t, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_") == "" {
		return t
	}
	return fmt.Sprintf("<%d bytes>", len(t))
}

// ---- UserScreening ----

type userScreening struct {
	toyohrpb.UnimplementedUserScreeningServer
}

// getViolation: a player with no violation gets NOT_FOUND, after the headers (rawunary.go). The
// game parses the name of the violation it is given as "tenants/{t}/users/{u}/violations/{v}" and
// aborts (2162-0001) on anything else, so an OK answer with no message, which the SDK turns into a
// default violation with an empty name, is a measured abort. OpenPak's bans are enforced at the
// gate, so every caller who gets this far has no violation.
func getViolation(_ any, ss grpc.ServerStream) error {
	var req toyohrpb.GetViolationRequest
	if err := ss.RecvMsg(&req); err != nil {
		return err
	}
	if err := sendEmptyHeaders(ss); err != nil {
		return err
	}
	return status.Error(codes.NotFound, "no violation")
}

// ---- maintenance ----

type maintenance struct {
	mtpb.UnimplementedMaintenanceScheduleServiceServer
}

// SubscribeMaintenanceSchedules: no maintenance is ever scheduled here, so the subscription only
// keeps itself alive.
func (m *maintenance) SubscribeMaintenanceSchedules(_ *mtpb.SubscribeMaintenanceSchedulesRequest, stream mtpb.MaintenanceScheduleService_SubscribeMaintenanceSchedulesServer) error {
	ka := &mtpb.SubscribeMaintenanceSchedulesResponse{Response: &mtpb.SubscribeMaintenanceSchedulesResponse_KeepAlive{KeepAlive: &mtpb.KeepAlive{}}}
	if err := stream.Send(ka); err != nil {
		return err
	}
	t := time.NewTicker(lobbyHeartbeat)
	defer t.Stop()
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-t.C:
			if err := stream.Send(ka); err != nil {
				return err
			}
		}
	}
}
