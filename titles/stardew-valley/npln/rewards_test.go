package npln

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	emit "github.com/openpak/rewards-emit"

	"github.com/openpak/npln/internal/counted"
	mmpb "github.com/openpak/npln/proto/matchmaking/v1"
)

func TestSessionClock(t *testing.T) {
	c := newSessionClock(3 * time.Minute)
	t0 := time.Unix(1000, 0)
	if d := c.tick(t0, map[string][]string{"g": {"a"}}); len(d) != 0 {
		t.Fatalf("a lone player counted: %v", d)
	}
	both := map[string][]string{"g": {"a", "b"}}
	c.tick(t0.Add(time.Minute), both)
	if d := c.tick(t0.Add(3*time.Minute), both); len(d) != 0 {
		t.Fatalf("counted after 2 minutes together: %v", d)
	}
	d := c.tick(t0.Add(4*time.Minute), both)
	if len(d) != 2 {
		t.Fatalf("want both players after 3 minutes, got %v", d)
	}
	if d := c.tick(t0.Add(9*time.Minute), both); len(d) != 0 {
		t.Fatalf("counted twice: %v", d)
	}
	// b leaves: a is alone, the timer resets; b rejoining starts over.
	c.tick(t0.Add(10*time.Minute), map[string][]string{"g": {"a"}})
	c.tick(t0.Add(11*time.Minute), both)
	if d := c.tick(t0.Add(12*time.Minute), both); len(d) != 0 {
		t.Fatalf("timer was not reset: %v", d)
	}
}

func TestOnlineSessionReachesRewards(t *testing.T) {
	var mu sync.Mutex
	var got []map[string]any
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var in struct{ Events []map[string]any }
		_ = json.Unmarshal(b, &in)
		mu.Lock()
		got = append(got, in.Events...)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer sink.Close()

	rememberPlayerIdentity("u-aaa", "00000000000000a1", 1)
	rememberPlayerIdentity("u-bbb", "00000000000000b2", 2)
	rw := emit.New(emit.Config{URL: sink.URL, Key: "k", Source: "t", Flush: 10 * time.Millisecond})
	defer rw.Close(t.Context())
	mm := newSessionServer()
	mm.members["room1"] = []*mmpb.UserSession{{User: "tenants/t/users/u-aaa"}}
	stop := mm.watchSessions(rw, "turf_sessions.s3", 300*time.Millisecond, 40*time.Millisecond, nil)
	defer stop()

	time.Sleep(400 * time.Millisecond) // alone in the room: nothing
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("a lone player counted: %v", got)
	}
	mu.Unlock()

	mm.mu.Lock()
	mm.members["room1"] = append(mm.members["room1"], &mmpb.UserSession{User: "tenants/t/users/u-bbb"})
	mm.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // no repeats
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("want one event per player, got %d: %v", len(got), got)
	}
	seen := map[string]bool{}
	for _, e := range got {
		if e["metric"] != "turf_sessions.s3" {
			t.Errorf("metric %v", e["metric"])
		}
		id, _ := e["identity"].(map[string]any)
		if id["namespace"] != "switch" {
			t.Errorf("identity %v", id)
		}
		seen[id["subject"].(string)] = true
		if k, _ := e["keys"].([]any); len(k) != 1 || k[0] != "room1" {
			t.Errorf("keys %v", e["keys"])
		}
	}
	if !seen["00000000000000a1"] || !seen["00000000000000b2"] {
		t.Errorf("subjects %v", seen)
	}
}

// A restart must not forget a counted session nor report it again: the server is rebuilt over the
// same durable store while both players are still in the room.
func TestCountedSessionSurvivesRestart(t *testing.T) {
	var mu sync.Mutex
	n := 0
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var in struct{ Events []map[string]any }
		_ = json.Unmarshal(b, &in)
		mu.Lock()
		n += len(in.Events)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer sink.Close()
	rememberPlayerIdentity("u-ccc", "00000000000000c3", 3)
	rememberPlayerIdentity("u-ddd", "00000000000000d4", 4)
	store, err := counted.Open(t.Context(), "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := func(wait time.Duration) {
		rw := emit.New(emit.Config{URL: sink.URL, Key: "k", Source: "t", Flush: 10 * time.Millisecond})
		mm := newSessionServer()
		mm.members["room9"] = []*mmpb.UserSession{{User: "tenants/t/users/u-ccc"}, {User: "tenants/t/users/u-ddd"}}
		stop := mm.watchSessions(rw, "game_sessions.x", 100*time.Millisecond, 20*time.Millisecond, store)
		time.Sleep(wait)
		stop()
		rw.Close(t.Context())
	}
	run(600 * time.Millisecond)
	mu.Lock()
	first := n
	mu.Unlock()
	if first != 2 {
		t.Fatalf("first run: want 2 events, got %d", first)
	}
	run(600 * time.Millisecond) // "restart": new server and clock, same store
	mu.Lock()
	defer mu.Unlock()
	if n != 2 {
		t.Fatalf("session reported again after restart: %d events", n)
	}
}
