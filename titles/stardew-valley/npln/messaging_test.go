package npln

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	msgpb "github.com/openpak/npln/proto/messaging/v1"
)

func openRecv(t *testing.T, m *messaging, pid uint64, uid, resume string) (*fakeStream[msgpb.RecvMessageResponse], context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(bearerCtx(pid, uid))
	s := newFakeStream[msgpb.RecvMessageResponse](ctx)
	go m.RecvMessage(&msgpb.RecvMessageRequest{User: "tenants/current/users/current", MessageResumeToken: resume}, s) //nolint:errcheck
	return s, cancel
}

// waitFor waits until the stream has sent at least n responses.
func waitFor[T any](t *testing.T, s *fakeStream[T], n int) []*T {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if got := s.got(); len(got) >= n {
			return got
		}
		select {
		case <-s.notify:
		case <-deadline:
			t.Fatalf("stream sent %d, want %d", len(s.got()), n)
		}
	}
}

func TestMessaging(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	m := newMessaging(newSessionServer())
	m.friends = func(ctx context.Context) (map[string]bool, error) {
		if _, uid, _ := bearer(ctx); uid == "u-a" || uid == "u-b" {
			return map[string]bool{"u-a": true, "u-b": true}, nil
		}
		return map[string]bool{}, nil
	}

	// The stream opens with a keep-alive announcing the observed 95 s idle timeout, and a
	// stream for someone else is refused.
	if err := m.RecvMessage(&msgpb.RecvMessageRequest{User: "tenants/current/users/u-b"}, newFakeStream[msgpb.RecvMessageResponse](bearerCtx(1, "u-a"))); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("stream for another user: %v", err)
	}
	bs, stopB := openRecv(t, m, 2, "u-b", "")
	if ka := waitFor(t, bs, 1)[0].GetKeepAlive(); ka.GetIdleTimeout().AsDuration() != 95*time.Second {
		t.Fatalf("opening keep-alive = %v", ka)
	}

	send := func(pid uint64, from, to, id string, ack bool) error {
		_, err := m.SendMessage(bearerCtx(pid, from), &msgpb.SendMessageRequest{User: "tenants/current/users/current", ReceiverUsers: []string{"tenants/current/users/" + to},
			MessageBody: &msgpb.MessageBody{MessageRequestId: id, NeedAck: ack, MessageType: "invite"}})
		return err
	}
	if err := send(3, "u-stranger", "u-b", "x", false); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("stranger messaged u-b: %v", err)
	}
	if err := send(1, "u-a", "u-b", "m1", true); err != nil {
		t.Fatal(err)
	}
	got := waitFor(t, bs, 2)[1].GetMessage()
	if got.GetMessageBody().GetMessageRequestId() != "m1" || lastSeg(got.GetSenderUser()) != "u-a" || got.GetMessageResumeToken() == "" {
		t.Fatalf("delivered = %v", got)
	}
	stopB()

	// Unacked, a need_ack message comes again on a fresh stream; after the resume token it does not.
	again, stop := openRecv(t, m, 2, "u-b", "")
	if waitFor(t, again, 2)[1].GetMessage().GetMessageBody().GetMessageRequestId() != "m1" {
		t.Fatal("unacked message not redelivered")
	}
	stop()
	resumed, stop := openRecv(t, m, 2, "u-b", got.GetMessageResumeToken())
	waitFor(t, resumed, 1)
	time.Sleep(50 * time.Millisecond)
	if n := len(resumed.got()); n != 1 {
		t.Fatalf("resumed stream got %d responses, want the keep-alive alone", n)
	}
	stop()

	// The ack reaches the sender naming the receiver; a forged ack by a third party does nothing.
	as, stopA := openRecv(t, m, 1, "u-a", "")
	defer stopA()
	waitFor(t, as, 1)
	if _, err := m.SendAck(bearerCtx(3, "u-stranger"), &msgpb.SendAckRequest{Acks: []*msgpb.AckBody{{MessageRequestId: "m1"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SendAck(bearerCtx(2, "u-b"), &msgpb.SendAckRequest{Acks: []*msgpb.AckBody{{MessageRequestId: "m1", SenderUser: "tenants/current/users/u-a"}}}); err != nil {
		t.Fatal(err)
	}
	all := waitFor(t, as, 2)
	if len(all) != 2 {
		t.Fatalf("sender got %d responses", len(all))
	}
	ack := all[1].GetAck()
	if ack.GetAck().GetMessageRequestId() != "m1" || lastSeg(ack.GetReceiverUser()) != "u-b" {
		t.Fatalf("ack = %v", ack)
	}
	m.mu.Lock()
	left := len(m.box("u-b").items)
	m.mu.Unlock()
	if left != 0 {
		t.Fatalf("acked message still queued (%d)", left)
	}

	// Body size is bounded.
	big := &msgpb.SendMessageRequest{ReceiverUsers: []string{"u-b"}, MessageBody: &msgpb.MessageBody{MessageType: string(make([]byte, msgMaxBody+1))}}
	if _, err := m.SendMessage(bearerCtx(1, "u-a"), big); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversized body: %v", err)
	}
}
