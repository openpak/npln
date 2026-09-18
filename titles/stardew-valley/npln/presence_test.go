package npln

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	friendspb "github.com/openpak/npln/proto/friends/v1"
)

// Dinkum's KeepAlive stream must be answered once per ping, through the real server.
func TestKeepAliveAnswersEveryPing(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(nil, "")
	go srv.Serve(lis)
	defer srv.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stream, err := friendspb.NewPresenceServiceClient(conn).KeepAlive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := stream.Send(&friendspb.KeepAliveRequest{}); err != nil {
			t.Fatal(err)
		}
		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("ping %d: %v", i, err)
		}
		if resp.GetHeartbeat().GetInterval().AsDuration() == 0 {
			t.Fatalf("ping %d: no heartbeat interval", i)
		}
	}
}
