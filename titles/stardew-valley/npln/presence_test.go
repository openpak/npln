package npln

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	friendspb "github.com/openpak/npln/proto/friends/v1"
)

// KeepAlive through the real server: opening heartbeat, timed heartbeats, updates answered,
// acks never answered.
func TestKeepAliveAnswersEveryPing(t *testing.T) {
	keepAliveBeat = 200 * time.Millisecond
	defer func() { keepAliveBeat = 10 * time.Second }()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(nil, "", AppID)
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
	// the server speaks first: a client that sends nothing still gets its heartbeat
	if resp, err := stream.Recv(); err != nil || resp.GetHeartbeat().GetInterval().AsDuration() == 0 {
		t.Fatalf("opening heartbeat: %v %v", resp, err)
	}
	got := make(chan *friendspb.KeepAliveResponse, 16)
	go func() {
		for {
			r, err := stream.Recv()
			if err != nil {
				close(got)
				return
			}
			got <- r
		}
	}()

	// acks are never answered: a console acks every heartbeat at once, and answering is a storm
	ack := &friendspb.KeepAliveRequest{Request: &friendspb.KeepAliveRequest_Ack{Ack: &friendspb.Ack{}}}
	for i := 0; i < 3; i++ {
		if err := stream.Send(ack); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case r := <-got:
		t.Fatalf("an ack was answered: %v", r)
	case <-time.After(keepAliveBeat / 2):
	}
	// the timer keeps the stream alive on its own
	select {
	case <-got:
	case <-time.After(keepAliveBeat * 2):
		t.Fatal("no timed heartbeat")
	}
	// a presence update is answered
	if err := stream.Send(&friendspb.KeepAliveRequest{Request: &friendspb.KeepAliveRequest_UpdatePresence_{
		UpdatePresence: &friendspb.KeepAliveRequest_UpdatePresence{Presence: &friendspb.Presence{}}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(keepAliveBeat / 2):
		t.Fatal("presence update not answered")
	}
}
