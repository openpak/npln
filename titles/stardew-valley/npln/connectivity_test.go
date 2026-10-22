package npln

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"context"
	"github.com/openpak/npln/internal/connectivity"
	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func configureConnectivity(t *testing.T, server string) {
	t.Helper()
	file := t.TempDir() + "/backend-token"
	if err := os.WriteFile(file, []byte(strings.Repeat("x", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NPLN_CONNECTIVITY_URL", server)
	t.Setenv("NPLN_CONNECTIVITY_TOKEN_FILE", file)
}

func TestSharedICEPreservesNPLNWireShapeAndSeparatePlayers(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/key.pem")
	var identities []connectivity.Identity
	var ttls []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
			t.Error("missing backend authorization")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/participants":
			var req struct {
				Key        connectivity.Identity `json:"key"`
				TTL        int                   `json:"ttl_seconds"`
				Candidates []any                 `json:"candidates"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			identities = append(identities, req.Key)
			ttls = append(ttls, req.TTL)
			json.NewEncoder(w).Encode(map[string]string{"id": req.Key.Player})
		case "/v1/ice":
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			json.NewEncoder(w).Encode(connectivity.ICE{STUN: "203.0.113.10:3478", TURN: "203.0.113.10:3478", Username: "lease:" + req["participant"], Password: "shared-password", ExpiresAt: time.Now().Add(30 * time.Minute)})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	configureConnectivity(t, server.URL)
	g := newSessionServer()
	for i := 1; i <= 2; i++ {
		uid := fmt.Sprintf("u-shared-ice-%d", i)
		ctx := bearerCtx(uint64(i), uid)
		set, err := g.AllocateIceServerSet(ctx, &mmpb.AllocateIceServerSetRequest{User: "tenants/foreign/users/u-victim"})
		if err != nil {
			t.Fatal(err)
		}
		if set.GetName() != Tenant+"/iceServerSets/static" || set.GetStunServer().GetHost() != "203.0.113.10" || set.GetTurnServers()[0].GetPassword() != "shared-password" || set.GetTurnServers()[0].GetUsername() != "lease:"+uid {
			t.Fatal("allocation shape or caller identity changed")
		}
		if set.GetTtl() == nil || set.GetTtl().AsDuration() != 0 || set.GetClientCacheDuration().AsDuration() != 90*time.Second || set.GetStunServer().GetProtocol() != mmpb.StunServer_UDP {
			t.Fatal("NPLN cache/TTL/protocol changed")
		}
	}
	if len(identities) != 2 || identities[0].Player == identities[1].Player || identities[0].Generation == identities[1].Generation {
		t.Fatal("players or bearer generations collapsed")
	}
	for i, key := range identities {
		if key.Title != lastSeg(Tenant) || key.Session != "ice-bootstrap" || key.Component != "pia-ice" || strings.Contains(key.Player, "victim") || ttls[i] < 1 || ttls[i] > 3600 {
			t.Fatal("untrusted scope or invalid TTL")
		}
	}
}

func TestSharedICEFailureNeverFallsBackToLocalSecret(t *testing.T) {
	t.Setenv("NPLN_JWT_KEY", t.TempDir()+"/key.pem")
	t.Setenv("NPLN_TURN_SECRET", "old-local-secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	configureConnectivity(t, server.URL)
	g := newSessionServer()
	if _, err := g.AllocateIceServerSet(bearerCtx(1, "u-one"), &mmpb.AllocateIceServerSetRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("service failure did not fail closed: %v", err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("uid", "u-victim"))
	if _, err := g.AllocateIceServerSet(ctx, &mmpb.AllocateIceServerSetRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("metadata identity reached connectivity")
	}
}
