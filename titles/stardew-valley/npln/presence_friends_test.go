package npln

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	commonpb "github.com/openpak/npln/proto/common"
	friendspb "github.com/openpak/npln/proto/friends/v1"
)

// A host's published presence reaches a friend's subscription, through the real server.
func TestFriendSeesHostPresence(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NX_INTERNAL_KEY", "k")
	accounts := map[uint64]string{
		1: `{"pid":1,"baas_user_id":"1111111111111111","friends":[{"pid":2,"baas_user_id":"2222222222222222"}]}`,
		2: `{"pid":2,"baas_user_id":"2222222222222222","friends":[{"pid":1,"baas_user_id":"1111111111111111"}]}`,
	}
	adapter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ PID uint64 }
		json.NewDecoder(r.Body).Decode(&in)
		fmt.Fprint(w, accounts[in.PID])
	}))
	defer adapter.Close()
	oldURL := adapterURL
	adapterURL = adapter.URL
	defer func() { adapterURL = oldURL }()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(nil, "", AppID)
	go srv.Serve(lis)
	defer srv.Stop()
	c, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pc := friendspb.NewPresenceServiceClient(c)
	as := func(pid uint64, baas string) context.Context {
		tok := newToken(pid, Tenant+"/users/"+userID(baas), Tenant, AppID)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		t.Cleanup(cancel)
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+tok.GetAccessToken())
	}

	// the host goes online and publishes it is hosting
	host, err := pc.KeepAlive(as(1, "1111111111111111"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Recv(); err != nil { // opening heartbeat
		t.Fatal(err)
	}
	attrs := map[string]*commonpb.Value{"status": {ValueType: &commonpb.Value_StringValue{StringValue: "host"}}}
	if err := host.Send(&friendspb.KeepAliveRequest{Request: &friendspb.KeepAliveRequest_UpdatePresence_{
		UpdatePresence: &friendspb.KeepAliveRequest_UpdatePresence{Presence: &friendspb.Presence{Attributes: attrs}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Recv(); err != nil { // the answer means it was recorded
		t.Fatal(err)
	}

	// the friend subscribes and sees the host online, hosting
	sub, err := pc.SubscribePresences(as(2, "2222222222222222"), &friendspb.SubscribePresencesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for {
		r, err := sub.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if r.GetEnumerationDone() != nil {
			t.Fatal("enumeration done without the host's presence")
		}
		for _, p := range r.GetPresences().GetPresences() {
			if p.GetState() == friendspb.State_ONLINE && p.GetAttributes()["status"].GetStringValue() == "host" {
				return
			}
		}
	}
}
