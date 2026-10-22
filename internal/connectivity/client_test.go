package connectivity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPrivateTokenConfiguration(t *testing.T) {
	t.Setenv("NPLN_CONNECTIVITY_URL", "")
	t.Setenv("NPLN_CONNECTIVITY_TOKEN_FILE", "")
	if c, err := FromEnvironment(); err != nil || c != nil {
		t.Fatal("disabled config failed")
	}
	file := t.TempDir() + "/token"
	if err := os.WriteFile(file, []byte(strings.Repeat("s", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NPLN_CONNECTIVITY_URL", "http://connectivity:20150")
	t.Setenv("NPLN_CONNECTIVITY_TOKEN_FILE", file)
	if c, err := FromEnvironment(); err != nil || c == nil {
		t.Fatal("valid config failed")
	}
	if err := os.Chmod(file, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := FromEnvironment(); !errors.Is(err, ErrUnavailable) {
		t.Fatal("public token accepted")
	}
}

func TestRedirectAndBadAllocationAreRejected(t *testing.T) {
	for _, reply := range []ICE{
		{STUN: "example:70000", TURN: "example:3478", Username: "u", Password: "p", ExpiresAt: time.Now().Add(time.Second)},
		{STUN: "example:3478", TURN: "example:3478", Username: "u", Password: "p", ExpiresAt: time.Now().Add(2 * time.Hour)},
		{STUN: "example:3478", TURN: "example:3478", Username: "u", Password: "p", ExpiresAt: time.Now().Add(-time.Second)},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/participants" {
				json.NewEncoder(w).Encode(map[string]string{"id": "one"})
				return
			}
			json.NewEncoder(w).Encode(reply)
		}))
		c, _ := New(server.URL, strings.Repeat("s", 32))
		_, err := c.Allocate(context.Background(), Identity{}, "", 60)
		server.Close()
		if !errors.Is(err, ErrUnavailable) {
			t.Fatal("bad allocation accepted")
		}
	}
	seen := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	c, _ := New(redirect.URL, strings.Repeat("s", 32))
	if _, err := c.Allocate(context.Background(), Identity{}, "", 60); !errors.Is(err, ErrUnavailable) || seen {
		t.Fatal("credential redirect followed")
	}
}
