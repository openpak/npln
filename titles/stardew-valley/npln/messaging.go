package npln

// messaging — nn.npln.messaging.v1.Messaging: player-to-player messages with optional acks.
//
// Observed (titles/super-mario-bros-wonder/docs/provenance.md): Wonder opens RecvMessage for
// "users/current" and abandons it unless the first answer is a keep-alive announcing a 95 s idle
// timeout and keep-alives follow every 45 s. Sending and acks have never been observed; what
// follows is read from the schema:
//
//   - SendMessage queues the body for each receiver and wakes their RecvMessage streams. A sender
//     may address only friends (nx-baas friend list) or players it shares a game session with.
//   - Each delivery carries a resume token; a stream opened with one gets only what came after.
//   - A need_ack message stays queued until the receiver acks it; the ack travels back to the
//     sender's stream as a RecvAck naming the receiver. Other messages go once delivered.
//
// ponytail: in-memory, bounded per user and by age; a restart drops undelivered messages.

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	msgpb "github.com/openpak/npln/proto/messaging/v1"
	"github.com/openpak/npln/redact"
)

const (
	msgIdleTimeout = 95 * time.Second // announced on the opening keep-alive (observed)
	msgMaxBody     = 16 << 10         // bytes of one encoded message body
	msgMaxTo       = 16               // receivers per SendMessage
	msgMaxQueued   = 100              // undelivered messages or acks per user
	msgMaxAge      = 10 * time.Minute // an undelivered message older than this is dropped
)

// msgBeat is the keep-alive cadence after the opening one (observed: 45 s).
var msgBeat = 45 * time.Second

type queued struct {
	seq  uint64
	at   time.Time
	from string // sender uid (a message) or receiver uid (an ack)
	msg  *msgpb.RecvMessage
	ack  *msgpb.RecvAck
}

type mailbox struct {
	items []*queued
	wake  map[chan struct{}]struct{}
}

type messaging struct {
	msgpb.UnimplementedMessagingServer
	rooms *sessionServer
	// friends lists a player's friends' user ids; nx-baas by default, a stub in tests.
	friends func(ctx context.Context) (map[string]bool, error)

	mu    sync.Mutex
	seq   uint64
	boxes map[string]*mailbox // uid -> mailbox
}

func newMessaging(rooms *sessionServer) *messaging {
	return &messaging{rooms: rooms, friends: callerFriends, boxes: map[string]*mailbox{}}
}

// callerFriends is the caller's friend set from nx-baas.
func callerFriends(ctx context.Context) (map[string]bool, error) {
	pid, ok := callerPID(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no bearer token for this call")
	}
	acct, err := lookupAccount(pid)
	if err != nil {
		log.Printf("[Msg] account lookup for pid=%s: %s", redact.Tag(pid), redact.Err(err))
		return nil, status.Error(codes.Unavailable, "friend list unavailable")
	}
	out := map[string]bool{}
	for _, f := range acct.Friends {
		out[f.UserID()] = true
	}
	return out, nil
}

// selfUser checks a request's `user` names the caller ("users/current" or the caller's id).
func selfUser(ctx context.Context, named string) (string, error) {
	user, err := callerUser(ctx)
	if err != nil {
		return "", err
	}
	if id := lastSeg(named); id != "" && id != "current" && id != lastSeg(user) {
		return "", status.Error(codes.PermissionDenied, "requested user does not match caller")
	}
	return lastSeg(user), nil
}

func (m *messaging) box(uid string) *mailbox {
	b := m.boxes[uid]
	if b == nil {
		b = &mailbox{wake: map[chan struct{}]struct{}{}}
		m.boxes[uid] = b
	}
	return b
}

// enqueue adds one item to uid's mailbox and wakes its streams. Caller holds m.mu.
func (m *messaging) enqueue(uid string, q *queued) {
	m.seq++
	q.seq, q.at = m.seq, time.Now()
	tok := strconv.FormatUint(q.seq, 10)
	if q.msg != nil {
		q.msg.MessageResumeToken = tok
	} else {
		q.ack.AckResumeToken = tok
	}
	b := m.box(uid)
	b.items = append(b.items, q)
	if len(b.items) > msgMaxQueued {
		b.items = b.items[len(b.items)-msgMaxQueued:]
	}
	for w := range b.wake {
		select {
		case w <- struct{}{}:
		default:
		}
	}
}

// sharesSession: both players are members of one live game session.
func (m *messaging) sharesSession(a, b string) bool {
	g := m.rooms
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, list := range g.members {
		if hasUID(list, a) && hasUID(list, b) {
			return true
		}
	}
	return false
}

func (m *messaging) SendMessage(ctx context.Context, req *msgpb.SendMessageRequest) (*msgpb.SendMessageResponse, error) {
	me, err := selfUser(ctx, req.GetUser())
	if err != nil {
		return nil, err
	}
	to := req.GetReceiverUsers()
	switch {
	case len(to) == 0:
		return nil, status.Error(codes.InvalidArgument, "no receiver")
	case len(to) > msgMaxTo:
		return nil, status.Errorf(codes.InvalidArgument, "at most %d receivers", msgMaxTo)
	case proto.Size(req.GetMessageBody()) > msgMaxBody:
		return nil, status.Errorf(codes.InvalidArgument, "message body over %d bytes", msgMaxBody)
	}
	ids := make([]string, len(to))
	for i, r := range to {
		if ids[i] = lastSeg(r); ids[i] == "current" {
			ids[i] = me
		}
	}
	var friends map[string]bool
	for _, id := range ids {
		if id == me || m.sharesSession(me, id) {
			continue
		}
		if friends == nil {
			if friends, err = m.friends(ctx); err != nil {
				return nil, err
			}
		}
		if !friends[id] {
			return nil, status.Errorf(codes.PermissionDenied, "%s is neither a friend nor in a session with the sender", id)
		}
	}
	body := proto.Clone(req.GetMessageBody()).(*msgpb.MessageBody)
	if body == nil {
		body = &msgpb.MessageBody{}
	}
	if body.MessageRequestId == "" {
		body.MessageRequestId = uuid4()
	}
	sender := tenantFromCtx(ctx) + "/users/" + me
	m.mu.Lock()
	for _, id := range ids {
		m.enqueue(id, &queued{from: me, msg: &msgpb.RecvMessage{MessageBody: body, SenderUser: sender, SendTime: timestamppb.Now()}})
	}
	m.mu.Unlock()
	log.Printf("[Msg] %s -> %d receiver(s) type=%s need_ack=%v", redact.Tag(me), len(to), redact.Tag(body.GetMessageType()), body.GetNeedAck())
	return &msgpb.SendMessageResponse{}, nil
}

// SendAck acknowledges messages the caller received: each drops the message from the caller's
// queue and reaches its sender as a RecvAck. An ack for a message the caller does not hold is
// ignored, so nobody can forge acks for others' messages.
func (m *messaging) SendAck(ctx context.Context, req *msgpb.SendAckRequest) (*msgpb.SendAckResponse, error) {
	me, err := selfUser(ctx, req.GetUser())
	if err != nil {
		return nil, err
	}
	receiver := tenantFromCtx(ctx) + "/users/" + me
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.box(me)
	for _, a := range req.GetAcks() {
		for i, q := range b.items {
			if q.msg == nil || !q.msg.GetMessageBody().GetNeedAck() || q.msg.GetMessageBody().GetMessageRequestId() != a.GetMessageRequestId() ||
				(a.GetSenderUser() != "" && lastSeg(a.GetSenderUser()) != q.from) {
				continue
			}
			b.items = append(b.items[:i:i], b.items[i+1:]...)
			m.enqueue(q.from, &queued{from: me, ack: &msgpb.RecvAck{
				Ack:          &msgpb.AckBody{MessageRequestId: a.GetMessageRequestId(), SenderUser: q.msg.GetSenderUser()},
				ReceiverUser: receiver,
				AckSendTime:  timestamppb.Now(),
			}})
			break
		}
	}
	return &msgpb.SendAckResponse{}, nil
}

// pending takes what uid's stream should deliver after the given resume points, dropping expired
// items and every delivered item that needs no ack. Caller holds m.mu.
func (m *messaging) pending(uid string, afterMsg, afterAck uint64) []*queued {
	b := m.box(uid)
	var out []*queued
	kept := b.items[:0]
	for _, q := range b.items {
		if time.Since(q.at) > msgMaxAge {
			continue
		}
		after := afterAck
		if q.msg != nil {
			after = afterMsg
		}
		if q.seq > after {
			out = append(out, q)
			if q.ack != nil || !q.msg.GetMessageBody().GetNeedAck() {
				continue // delivered once, gone
			}
		}
		kept = append(kept, q)
	}
	for i := len(kept); i < len(b.items); i++ {
		b.items[i] = nil
	}
	b.items = kept
	return out
}

func keepAlive(d time.Duration) *msgpb.RecvMessageResponse {
	return &msgpb.RecvMessageResponse{Reply: &msgpb.RecvMessageResponse_KeepAlive{KeepAlive: &msgpb.KeepAlive{IdleTimeout: durationpb.New(d)}}}
}

func (m *messaging) RecvMessage(req *msgpb.RecvMessageRequest, stream grpc.ServerStreamingServer[msgpb.RecvMessageResponse]) error {
	ctx := stream.Context()
	me, err := selfUser(ctx, req.GetUser())
	if err != nil {
		return err
	}
	afterMsg, _ := strconv.ParseUint(req.GetMessageResumeToken(), 10, 64)
	afterAck, _ := strconv.ParseUint(req.GetAckResumeToken(), 10, 64)
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return err
	}
	if err := stream.Send(keepAlive(msgIdleTimeout)); err != nil {
		return err
	}
	wake := make(chan struct{}, 1)
	m.mu.Lock()
	m.box(me).wake[wake] = struct{}{}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.box(me).wake, wake)
		m.mu.Unlock()
	}()
	log.Printf("[Msg] RecvMessage open for %s", redact.Tag(me))

	beat := time.NewTicker(msgBeat)
	defer beat.Stop()
	for {
		m.mu.Lock()
		out := m.pending(me, afterMsg, afterAck)
		m.mu.Unlock()
		for _, q := range out {
			r := &msgpb.RecvMessageResponse{Reply: &msgpb.RecvMessageResponse_Message{Message: q.msg}}
			if q.ack != nil {
				r = &msgpb.RecvMessageResponse{Reply: &msgpb.RecvMessageResponse_Ack{Ack: q.ack}}
				afterAck = q.seq
			} else {
				afterMsg = q.seq
			}
			if err := stream.Send(r); err != nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			log.Printf("[Msg] RecvMessage closed for %s", redact.Tag(me))
			return nil
		case <-wake:
		case <-beat.C:
			if stream.Send(keepAlive(0)) != nil {
				return nil
			}
		}
	}
}
