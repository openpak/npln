package npln

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/openpak/npln/bans"
	hydropb "github.com/openpak/npln/proto/hydro/v1"
	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
	msgpb "github.com/openpak/npln/proto/messaging/v1"
	ugcpb "github.com/openpak/npln/proto/ugcstore/v1"
)

func serve(t *testing.T, opts Options) *grpc.ClientConn {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(nil, "", AppID, opts)
	go srv.Serve(lis) //nolint:errcheck
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// The new services exist only for a title that asks for them, and the ban gate covers them.
func TestNewServicesAreOptInAndBanned(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/k.pem")
	t.Setenv("NX_INTERNAL_KEY", "k")
	t.Setenv("NPLN_UGC_FILE", t.TempDir()+"/ugc.json")
	defer func() { titleOptions = Options{} }()
	var banned atomic.Bool
	adapter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if banned.Load() {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":"account_banned"}`)
			return
		}
		fmt.Fprint(w, `{"pid":7,"baas_user_id":"7777777777777777","friends":[]}`)
	}))
	defer adapter.Close()
	oldURL, oldGate := adapterURL, bans.Default
	adapterURL, bans.Default = adapter.URL, bans.New(adapter.URL, "k")
	defer func() { adapterURL, bans.Default = oldURL, oldGate }()

	tok := newToken(7, Tenant+"/users/"+userID("7777777777777777"), Tenant, AppID)
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+tok.AccessToken)
	ticket := &mmpb.CreateMatchmakingTicketRequest{MatchmakingTicket: &mmpb.MatchmakingTicket{MatchmakingConfig: "CourseMatch_20221202", UserDefinitions: []*mmpb.UserDefinition{{User: "users/current"}}}}

	plain := serve(t, Options{})
	if _, err := mmpb.NewMatchmakerClient(plain).CreateMatchmakingTicket(ctx, ticket); status.Code(err) != codes.Unimplemented {
		t.Fatalf("Matchmaker on a title without it: %v", err)
	}

	conn := serve(t, Options{ConfigProperties: true, Matchmaker: true, Messaging: true, UGC: true, Hydro: true})
	calls := map[string]func() error{
		"Matchmaker": func() error {
			_, err := mmpb.NewMatchmakerClient(conn).CreateMatchmakingTicket(ctx, ticket)
			return err
		},
		"Messaging": func() error {
			_, err := msgpb.NewMessagingClient(conn).SendMessage(ctx, &msgpb.SendMessageRequest{ReceiverUsers: []string{"users/current"}})
			return err
		},
		"Ugcstore": func() error {
			_, err := ugcpb.NewUgcstoreClient(conn).CommitDocuments(ctx, &ugcpb.CommitDocumentsRequest{})
			return err
		},
		"Screening": func() error {
			_, err := ugcpb.NewScreeningClient(conn).CreateReport(ctx, &ugcpb.CreateReportRequest{})
			return err
		},
		"Datastore": func() error {
			_, err := hydropb.NewDatastoreClient(conn).GetContent(ctx, &hydropb.GetContentRequest{Name: "x"})
			return err
		},
		"RecvMessage": func() error {
			s, err := msgpb.NewMessagingClient(conn).RecvMessage(ctx, &msgpb.RecvMessageRequest{User: "users/current"})
			if err != nil {
				return err
			}
			_, err = s.Recv()
			return err
		},
	}
	for name, call := range calls {
		if err := call(); err != nil {
			t.Fatalf("%s for an active account: %v", name, err)
		}
	}
	banned.Store(true)
	bans.Default = bans.New(adapter.URL, "k") // forget the cached active answer
	conn = serve(t, Options{ConfigProperties: true, Matchmaker: true, Messaging: true, UGC: true, Hydro: true})
	for name, call := range calls {
		if err := call(); !bans.IsRefused(err) {
			t.Fatalf("%s for a banned account: want the banned refusal, got %v", name, err)
		}
	}
}
