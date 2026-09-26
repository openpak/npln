package online

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReportPayload(t *testing.T) {
	var got []byte
	var key, path string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		key, path = r.Header.Get("X-Internal-Key"), r.URL.Path
		got, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"marked":2,"unknown":0}`))
	}))
	defer srv.Close()

	r := New(srv.URL+"/", "k")
	r.Start("0100E65002BB8000")
	r.Report()
	if calls != 0 {
		t.Fatalf("nobody online still posted")
	}
	a := r.Join(1800000002)
	b := r.Join(1800000001)
	b2 := r.Join(1800000001) // a second stream from the same console
	_ = r.Join(0)
	b()
	b() // leave is idempotent
	r.Report()
	if path != "/internal/switch/online" || key != "k" {
		t.Fatalf("posted %s with key %q", path, key)
	}
	want := `{"title_id":"0100e65002bb8000","players":[{"pid":1800000001},{"pid":1800000002}]}`
	if string(got) != want {
		t.Fatalf("body\n got %s\nwant %s", got, want)
	}
	a()
	b2()
	calls = 0
	r.Report()
	if calls != 0 {
		t.Fatalf("everyone left, still posted")
	}
	var p payload
	if json.Unmarshal(got, &p) != nil || len(p.Players) != 2 {
		t.Fatalf("payload does not decode: %s", got)
	}
}

func TestUnsetIsNoop(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	for _, r := range []*Reporter{New(srv.URL, ""), New("", "k")} {
		r.Start("0100e65002bb8000")
		defer r.Join(1800000001)()
		r.Report()
	}
	if calls != 0 {
		t.Fatalf("unconfigured reporter posted %d time(s)", calls)
	}
}

func TestFailureNeverPanics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	r := New(srv.URL, "k")
	r.Start("0100e65002bb8000")
	defer r.Join(1)()
	r.Report()
	r.Report() // second failure inside the minute stays quiet
}
