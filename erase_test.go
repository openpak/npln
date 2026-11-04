package npln

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeLinks map[string][]string

func (f fakeLinks) SwitchSubjects(_ context.Context, id string) ([]string, error) { return f[id], nil }

const (
	acctA = "11111111-1111-1111-1111-111111111111"
	acctB = "22222222-2222-2222-2222-222222222222"
)

func TestEraseAccountResolvesUserIDs(t *testing.T) {
	var got []string
	RegisterEraser(func(_ context.Context, uids []string) (int, error) { got = append(got, uids...); return len(uids), nil })
	links := fakeLinks{acctA: {"aaaaaaaaaaaa0001"}, acctB: {"bbbbbbbbbbbb0002"}}
	n, err := EraseAccount(context.Background(), links, "npln-test", acctA)
	if err != nil || n != 1 || len(got) != 1 || got[0] != UserID("aaaaaaaaaaaa0001") {
		t.Fatalf("n=%d err=%v got=%v", n, err, got)
	}
	if len(UserID("x")) != 22 {
		t.Fatalf("user id shape %q", UserID("x"))
	}
}

func TestEraseEndpointAuth(t *testing.T) {
	links := fakeLinks{acctA: {"aaaaaaaaaaaa0001"}}
	call := func(key, hdr, id string) int {
		mux := http.NewServeMux()
		mux.HandleFunc("DELETE /internal/accounts/{account_id}/data", EraseHandler(key, "npln-test", links))
		r := httptest.NewRequest("DELETE", "/internal/accounts/"+id+"/data", nil)
		if hdr != "" {
			r.Header.Set("Authorization", hdr)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	for _, c := range []struct {
		key, hdr, id string
		want         int
	}{
		{"", "Bearer k", acctA, 503},
		{"k", "", acctA, 401},
		{"k", "Bearer wrong", acctA, 401},
		{"k", "Bearer k", "not-a-uuid", 400},
		{"k", "Bearer k", acctA, 200},
	} {
		if got := call(c.key, c.hdr, c.id); got != c.want {
			t.Errorf("%+v: got %d", c, got)
		}
	}
}
