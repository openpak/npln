package npln

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/openpak/npln/internal/blob"
	commonpb "github.com/openpak/npln/proto/common"
	hydropb "github.com/openpak/npln/proto/hydro/v1"
	msgpb "github.com/openpak/npln/proto/messaging/v1"
)

// reopen is a restart: a fresh connection to the same database and title. Needs a scratch
// database (NPLN_TEST_DSN, like internal/blob); skipped without one.
func testBlob(t *testing.T) func() *blob.Store {
	t.Helper()
	dsn := os.Getenv("NPLN_TEST_DSN")
	if dsn == "" {
		t.Skip("NPLN_TEST_DSN not set")
	}
	title := "t-persist-" + t.Name()
	clean := func() {
		s, err := blob.Open(context.Background(), dsn, title)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		for _, k := range []string{hydroKind, msgKind, msgSeqKind} {
			all, _ := s.List(k)
			for key := range all {
				s.DeleteKeys(k, []string{key})
			}
		}
	}
	clean()
	t.Cleanup(clean)
	return func() *blob.Store {
		s, err := blob.Open(context.Background(), dsn, title)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		return s
	}
}

func TestHydroSurvivesRestart(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	open := testBlob(t)
	a := bearerCtx(1, "u-a")
	h := newHydroDB(open())
	c, err := h.CreateContent(a, &hydropb.CreateContentRequest{Parent: "tenants/current", Content: &hydropb.Content{Payload: []byte("hi")}})
	if err != nil {
		t.Fatal(err)
	}
	set := &hydropb.TransformContentRequest{Name: c.GetName(), Etag: c.GetEtag(), FieldTransforms: []*hydropb.FieldTransform{
		{FieldPath: "payload", TransformType: &hydropb.FieldTransform_Set{Set: &commonpb.Value{ValueType: &commonpb.Value_BytesValue{BytesValue: []byte("new")}}}},
		{FieldPath: "attributes.3", TransformType: &hydropb.FieldTransform_Increment{Increment: gsInt(2)}},
	}}
	if _, err := h.TransformContent(a, set); err != nil {
		t.Fatal(err)
	}
	before, _ := h.GetContent(a, &hydropb.GetContentRequest{Name: c.GetName()})
	gone, _ := h.CreateContent(a, &hydropb.CreateContentRequest{Parent: "tenants/current", Content: &hydropb.Content{Payload: []byte("x")}})
	if _, err := h.DeleteContent(a, &hydropb.DeleteContentRequest{Name: gone.GetName()}); err != nil {
		t.Fatal(err)
	}

	h2 := newHydroDB(open()) // restart
	got, _ := h2.GetContent(a, &hydropb.GetContentRequest{Name: c.GetName()})
	if string(got.GetPayload()) != "new" || got.GetAttributes()[3] != 2 || got.GetEtag() != before.GetEtag() || got.GetUser() != c.GetUser() {
		t.Fatalf("after restart: %v, want %v", got, before)
	}
	if miss, _ := h2.GetContent(a, &hydropb.GetContentRequest{Name: gone.GetName()}); miss.GetPayload() != nil {
		t.Fatal("deleted content came back")
	}
	// The kept etag still guards writes, and the next revision continues from it.
	set.Etag = before.GetEtag()
	if _, err := h2.TransformContent(a, set); err != nil {
		t.Fatalf("write with the pre-restart etag: %v", err)
	}
	after, _ := h2.GetContent(a, &hydropb.GetContentRequest{Name: c.GetName()})
	n, _ := strconv.Atoi(after.GetEtag())
	b, _ := strconv.Atoi(before.GetEtag())
	if n != b+1 {
		t.Fatalf("etag %s after %s", after.GetEtag(), before.GetEtag())
	}
}

func TestMessagingSurvivesRestart(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	open := testBlob(t)
	friends := func(context.Context) (map[string]bool, error) { return map[string]bool{"u-a": true, "u-b": true}, nil }
	m := newMessagingDB(newSessionServer(), open())
	m.friends = friends
	send := func(m *messaging, id string) {
		t.Helper()
		_, err := m.SendMessage(bearerCtx(1, "u-a"), &msgpb.SendMessageRequest{User: "tenants/current/users/current", ReceiverUsers: []string{"tenants/current/users/u-b"},
			MessageBody: &msgpb.MessageBody{MessageRequestId: id, NeedAck: true, MessageType: "invite"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	send(m, "m1")
	send(m, "m2")
	if _, err := m.SendAck(bearerCtx(2, "u-b"), &msgpb.SendAckRequest{User: "tenants/current/users/current", Acks: []*msgpb.AckBody{{MessageRequestId: "m2", SenderUser: "tenants/current/users/u-a"}}}); err != nil {
		t.Fatal(err)
	}

	m2 := newMessagingDB(newSessionServer(), open()) // restart
	m2.friends = friends
	// m1 waits for its ack; m2 is acked and gone; u-a's RecvAck for m2 waits too.
	bs, stop := openRecv(t, m2, 2, "u-b", "")
	defer stop()
	got := waitFor(t, bs, 2)
	if got[1].GetMessage().GetMessageBody().GetMessageRequestId() != "m1" {
		t.Fatalf("after restart u-b got %v", got[1])
	}
	m1tok := got[1].GetMessage().GetMessageResumeToken()
	as, stopA := openRecv(t, m2, 1, "u-a", "")
	defer stopA()
	if ack := waitFor(t, as, 2)[1].GetAck(); ack.GetAck().GetMessageRequestId() != "m2" {
		t.Fatalf("sender's ack lost: %v", ack)
	}
	// Resume tokens keep rising across the restart.
	send(m2, "m3")
	m3 := waitFor(t, bs, 3)[2].GetMessage()
	a, _ := strconv.ParseUint(m1tok, 10, 64)
	b, _ := strconv.ParseUint(m3.GetMessageResumeToken(), 10, 64)
	if m3.GetMessageBody().GetMessageRequestId() != "m3" || b <= a+1 {
		t.Fatalf("m3 token %d after m1 %d", b, a)
	}
}

// Account erasure reaches the durable copies, not only the live maps.
func TestEraseReachesDurableState(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NPLN_DATABASE_URL", "")
	t.Setenv("NPLN_STATE_DIR", t.TempDir())
	open := testBlob(t)
	h := trackedHydro(newHydroDB(open()))
	c, err := h.CreateContent(bearerCtx(1, "u-a"), &hydropb.CreateContentRequest{Parent: "tenants/current", Content: &hydropb.Content{Payload: []byte("hi")}})
	if err != nil {
		t.Fatal(err)
	}
	m := trackedMessaging(newSessionServer())
	m.db = open()
	m.friends = func(context.Context) (map[string]bool, error) { return map[string]bool{"u-a": true, "u-b": true}, nil }
	if _, err := m.SendMessage(bearerCtx(1, "u-a"), &msgpb.SendMessageRequest{User: "tenants/current/users/current", ReceiverUsers: []string{"tenants/current/users/u-b"},
		MessageBody: &msgpb.MessageBody{MessageRequestId: "m1", NeedAck: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := eraseUsers(context.Background(), []string{"u-a"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := open().List(hydroKind); len(got) != 0 {
		t.Fatalf("hydro records after erase: %d (%s)", len(got), c.GetName())
	}
	if got, _ := open().List(msgKind); len(got) != 0 {
		t.Fatalf("message records after erase: %d", len(got))
	}
}
