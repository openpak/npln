package npln

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
	stardew "github.com/openpak/npln/titles/stardew-valley/npln"
)

// Exercise the registered RPC, including its shared service implementation and endpoint config.
func TestLatencyDiscoveryThroughServer(t *testing.T) {
	t.Setenv("NPLN_STUN_HOST", "192.0.2.10")
	t.Setenv("NPLN_STUN_PORT", "3478")
	t.Setenv("NPLN_LATENCY_HOST", "")
	t.Setenv("NPLN_LATENCY_PORT", "")
	wantIDs := []string{"eu-01-udp", "eu-02-udp", "us-01-udp", "us-02-udp", "jp-01-udp", "sg-01-udp", "au-01-udp"}
	wantRegions := []string{"europe-central2", "europe-west2", "northamerica-northeast1", "us-west2", "asia-northeast1", "asia-southeast1", "australia-southeast1"}
	for _, override := range []bool{false, true} {
		name, host, port := "stun-default", "192.0.2.10", int32(3478)
		if override {
			name, host, port = "latency-override", "192.0.2.20", 3479
			t.Setenv("NPLN_LATENCY_HOST", host)
			t.Setenv("NPLN_LATENCY_PORT", "3479")
		}
		t.Run(name, func(t *testing.T) {
			lis, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			srv := NewServer(nil, nil)
			go srv.Serve(lis)
			defer srv.Stop()
			conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var headers metadata.MD
			resp, err := mmpb.NewGameSessionServiceClient(conn).ListLatencyMeasurementServers(ctx,
				&mmpb.ListLatencyMeasurementServersRequest{Parent: "tenants/current"}, grpc.Header(&headers))
			if err != nil {
				t.Fatal(err)
			}
			if got := headers.Get("npln-grpc-type"); len(got) != 1 || got[0] != "Unary" {
				t.Fatalf("response type header: %v", got)
			}
			if len(resp.GetLatencyMeasurementServers()) != len(wantIDs) {
				t.Fatalf("got %d candidates, want %d", len(resp.GetLatencyMeasurementServers()), len(wantIDs))
			}
			for i, server := range resp.GetLatencyMeasurementServers() {
				if server.GetName() != Tenant+"/latencyMeasurementServers/"+wantIDs[i] || server.GetRegion() != wantRegions[i] ||
					server.GetHost() != host || server.GetPort() != port || server.GetProtocol() != mmpb.LatencyMeasurementServer_UDP {
					t.Fatalf("candidate %d has wrong wire values: %v", i, server)
				}
			}
		})
	}

	// A later registration for another title must return the ordinary one-candidate list.
	srv := stardew.NewServer(nil, stardew.StardewTenant, stardew.AppID)
	defer srv.Stop()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(lis)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := mmpb.NewGameSessionServiceClient(conn).ListLatencyMeasurementServers(ctx,
		&mmpb.ListLatencyMeasurementServersRequest{Parent: "tenants/current"})
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.GetLatencyMeasurementServers(); len(got) != 1 ||
		got[0].GetName() != stardew.StardewTenant+"/latencyMeasurementServers/eu-01-udp" || got[0].GetRegion() != "europe-west2" {
		t.Fatalf("Splatoon candidate profile leaked into another title: %v", got)
	}
}
