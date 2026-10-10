package npln

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonpb "github.com/openpak/npln/proto/common"
	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
)

// One check over the boot-time toyohr calls, on a real server: GetViolation is NOT_FOUND with the
// Unary type header; a player with no save gets NOT_FOUND, uploads one, and reads
// it back named under the tenant and tagged; a partial write merges maps, replaces the rest and
// stores an empty array as null; and the NplnLogin a player sends itself comes back on its
// RecvMessage after the opening KeepAlive, carrying a later cursor and the resolved sender name.
func TestBootServices(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	saves.dir = t.TempDir()
	defer func() { saves.dir = "" }()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(nil, nil)
	go srv.Serve(lis)
	defer srv.Stop()
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	tok := newToken(7, Tenant+"/users/u-player", Tenant).GetAccessToken()
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+tok), 10*time.Second)
	defer cancel()

	// GetViolation: OK, zero messages.
	st, err := cc.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, "/nn.npln.toyohr.v1.UserScreening/GetViolation")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SendMsg(&toyohrpb.GetViolationRequest{Name: "tenants/current/users/u-player/violation"}); err != nil {
		t.Fatal(err)
	}
	st.CloseSend()
	if h, _ := st.Header(); len(h.Get("npln-grpc-type")) != 1 || h.Get("npln-grpc-type")[0] != "Unary" {
		t.Fatalf("GetViolation headers: %v", h)
	}
	if err := st.RecvMsg(&toyohrpb.GetViolationResponse{}); status.Code(err) != codes.NotFound {
		t.Fatalf("GetViolation: want NOT_FOUND, got %v", err)
	}

	cs := toyohrpb.NewCloudSaveClient(cc)
	if _, err := cs.GetSaveRecord(ctx, &toyohrpb.GetSaveRecordRequest{Name: "tenants/current/saveRecords/current"}); status.Code(err) != codes.NotFound {
		t.Fatalf("no record: want NOT_FOUND, got %v", err)
	}
	local := &commonpb.MapValue{Fields: map[string]*commonpb.Value{
		"UserName": sv("Inkling"), "Money": iv(0),
		"Gear": {ValueType: &commonpb.Value_MapValue{MapValue: &commonpb.MapValue{Fields: map[string]*commonpb.Value{"a": iv(1), "b": iv(2)}}}},
	}}
	created, err := cs.CreateSaveRecord(ctx, &toyohrpb.CreateSaveRecordRequest{Parent: "tenants/current", SaveRecord: &toyohrpb.SaveRecord{SaveData: local}})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetName() != Tenant+"/saveRecords/u-player" || len(created.GetSaveData().GetFields()["Identifier"].GetStringValue()) != 4 {
		t.Fatalf("created record: %v", created)
	}
	delta := &commonpb.MapValue{Fields: map[string]*commonpb.Value{
		"Money": iv(500),
		"Gear":  {ValueType: &commonpb.Value_MapValue{MapValue: &commonpb.MapValue{Fields: map[string]*commonpb.Value{"b": iv(3)}}}},
		"Skills": {ValueType: &commonpb.Value_ArrayValue{ArrayValue: &commonpb.ArrayValue{}}},
	}}
	w, err := cs.WriteSaveRecord(ctx, &toyohrpb.WriteSaveRecordRequest{Name: "tenants/current/saveRecords/current",
		UpdateTime: created.GetUpdateTime(), SaveEventType: "tenants/current/saveEventTypes/Test", SaveRecord: &toyohrpb.SaveRecord{SaveData: delta}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := cs.GetSaveRecord(ctx, &toyohrpb.GetSaveRecordRequest{Name: "tenants/current/saveRecords/current"})
	if err != nil {
		t.Fatal(err)
	}
	f := got.GetSaveData().GetFields()
	gear := f["Gear"].GetMapValue().GetFields()
	if f["Money"].GetIntegerValue() != 500 || gear["a"].GetIntegerValue() != 1 || gear["b"].GetIntegerValue() != 3 ||
		f["Skills"].GetNullValue() != 0 || f["Skills"].GetValueType() == nil || !got.GetUpdateTime().AsTime().Equal(w.GetTimestamp().AsTime()) {
		t.Fatalf("merged record: %v", got)
	}

	lm := toyohrpb.NewLobbyMessagingClient(cc)
	rs, err := lm.RecvMessage(ctx, &toyohrpb.RecvMessageRequest{User: "tenants/current/users/u-player"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := rs.Recv()
	if err != nil || first.GetKeepAlive() == nil {
		t.Fatalf("opening frame: %v %v", first, err)
	}
	if _, err := lm.SendMessage(ctx, &toyohrpb.SendMessageRequest{User: "tenants/current/users/u-player",
		ReceiverUsers: []string{"tenants/current/users/u-player"}, MessageBody: &toyohrpb.MessageBody{MessageType: "NplnLogin"}}); err != nil {
		t.Fatal(err)
	}
	m, err := rs.Recv()
	if err != nil || m.GetMessage().GetMessageBody().GetMessageType() != "NplnLogin" ||
		m.GetMessage().GetSenderUser() != Tenant+"/users/u-player" ||
		m.GetMessage().GetMessageResumeToken() <= first.GetKeepAlive().GetMessageResumeToken() {
		t.Fatalf("echo: %v %v", m, err)
	}
}
