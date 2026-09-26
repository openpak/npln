// Package online tells nx-baas who is playing, so openpak.org counts playtime only while a game
// server confirms the player online. Every 30 s it posts the PIDs holding a presence KeepAlive
// open to POST {NX_INTERNAL_URL}/internal/switch/online. No NX_INTERNAL_KEY: it does nothing.
// A failure is logged at most once a minute and never reaches the game.
package online

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const every = 30 * time.Second

// Reporter holds the players online on one title and reports them.
type Reporter struct {
	url, key string
	client   *http.Client
	now      func() time.Time

	once    sync.Once
	mu      sync.Mutex
	title   string
	pids    map[uint64]int // pid -> open KeepAlive streams (a console may hold two)
	lastLog time.Time
}

// New builds a reporter; an empty url or key makes it a no-op.
func New(url, key string) *Reporter {
	return &Reporter{
		url: strings.TrimRight(url, "/"), key: key,
		client: &http.Client{Timeout: 5 * time.Second},
		now:    time.Now, pids: map[uint64]int{},
	}
}

// Default uses the variables every title already reaches nx-baas with (same default URL).
var Default = New(envOr("NX_INTERNAL_URL", "http://127.0.0.1:20070"), os.Getenv("NX_INTERNAL_KEY"))

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func (r *Reporter) enabled() bool { return r.url != "" && r.key != "" }

// Start names the title this process serves (16 hex digits) and begins reporting every 30 s.
// Later calls are ignored: one process serves one title.
func (r *Reporter) Start(titleID string) {
	if !r.enabled() {
		return
	}
	r.once.Do(func() {
		r.mu.Lock()
		r.title = strings.ToLower(titleID)
		r.mu.Unlock()
		go func() {
			for range time.Tick(every) {
				r.Report()
			}
		}()
	})
}

// Join counts pid online until the returned func is called. pid 0 is nobody.
func (r *Reporter) Join(pid uint64) (leave func()) {
	if pid == 0 {
		return func() {}
	}
	r.mu.Lock()
	r.pids[pid]++
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			if r.pids[pid]--; r.pids[pid] <= 0 {
				delete(r.pids, pid)
			}
			r.mu.Unlock()
		})
	}
}

type player struct {
	PID uint64 `json:"pid"`
}

type payload struct {
	TitleID string   `json:"title_id"`
	Players []player `json:"players"`
}

func (r *Reporter) snapshot() (payload, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.title == "" || len(r.pids) == 0 {
		return payload{}, false
	}
	p := payload{TitleID: r.title, Players: make([]player, 0, len(r.pids))}
	for pid := range r.pids {
		p.Players = append(p.Players, player{PID: pid})
	}
	sort.Slice(p.Players, func(i, j int) bool { return p.Players[i].PID < p.Players[j].PID })
	return p, true
}

// Report posts everyone online now. Nobody online, or no nx-baas configured: no request.
func (r *Reporter) Report() {
	if !r.enabled() {
		return
	}
	p, ok := r.snapshot()
	if !ok {
		return
	}
	if err := r.post(p); err != nil {
		r.mu.Lock()
		quiet := r.now().Sub(r.lastLog) < time.Minute
		if !quiet {
			r.lastLog = r.now()
		}
		r.mu.Unlock()
		if !quiet {
			log.Printf("[Online] report for %s (%d player(s)): %v", p.TitleID, len(p.Players), err)
		}
	}
}

func (r *Reporter) post(p payload) error {
	body, _ := json.Marshal(p)
	req, err := http.NewRequest(http.MethodPost, r.url+"/internal/switch/online", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", r.key)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("nx-baas answered %s", resp.Status)
	}
	return nil
}
