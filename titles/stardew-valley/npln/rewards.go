package npln

// Online sessions for rewards: a player counts once they have shared a game session (a room, a
// private battle, a matchmaker pool) with at least one other player for sessionMin of unbroken
// time. It proves presence in an online lobby together, not a finished match. One event per
// (game session, player); the session id is the event id and the distinct key.

import (
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	emit "github.com/openpak/rewards-emit"
)

// sessionMin is how long two players must share a session before it counts.
func sessionMin() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("REWARDS_SESSION_MIN")); err == nil && d > 0 {
		return d
	}
	return 3 * time.Minute
}

// Who a user id belongs to: the BAAS user id (the "switch" identity subject) once login proved
// it, else the pid of a bearer seen on a call, resolved through the adapter on demand.
var players = struct {
	sync.Mutex
	baas map[string]string
	pid  map[string]uint64
}{baas: map[string]string{}, pid: map[string]uint64{}}

func rememberPlayerIdentity(uid, baas string, pid uint64) {
	players.Lock()
	defer players.Unlock()
	players.baas[uid], players.pid[uid] = baas, pid
}

func notePlayerPID(uid string, pid uint64) {
	players.Lock()
	defer players.Unlock()
	if _, ok := players.pid[uid]; !ok {
		players.pid[uid] = pid
	}
}

// baasOf resolves a user id to its BAAS user id, "" when unknown.
func baasOf(uid string) string {
	players.Lock()
	b, pid := players.baas[uid], players.pid[uid]
	players.Unlock()
	if b != "" || pid == 0 {
		return b
	}
	acc, err := lookupAccount(pid)
	if err != nil || acc.UserID() != uid {
		return ""
	}
	players.Lock()
	players.baas[uid] = acc.BaasUserID
	players.Unlock()
	return acc.BaasUserID
}

type sessionPair struct{ gsid, uid string }

type sessionClock struct {
	min     time.Duration
	since   map[string]time.Time
	emitted map[string]bool
}

func newSessionClock(min time.Duration) *sessionClock {
	return &sessionClock{min: min, since: map[string]time.Time{}, emitted: map[string]bool{}}
}

// tick takes the sessions now (id -> participant user ids) and returns the pairs that just
// crossed the minimum. A session with fewer than two players resets its players.
func (c *sessionClock) tick(now time.Time, gs map[string][]string) (done []sessionPair) {
	seen := map[string]bool{}
	for g, uids := range gs {
		if len(uids) < 2 {
			continue
		}
		for _, uid := range uids {
			k := g + "/" + uid
			seen[k] = true
			if _, ok := c.since[k]; !ok {
				c.since[k] = now
			}
			if !c.emitted[k] && now.Sub(c.since[k]) >= c.min {
				c.emitted[k] = true
				done = append(done, sessionPair{g, uid})
			}
		}
	}
	for k := range c.since {
		if !seen[k] {
			delete(c.since, k)
		}
	}
	for k := range c.emitted {
		if !seen[k] {
			delete(c.emitted, k)
		}
	}
	sort.Slice(done, func(i, j int) bool { return done[i].gsid+done[i].uid < done[j].gsid+done[j].uid })
	return done
}

func (g *sessionServer) sessionSnapshot() map[string][]string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[string][]string, len(g.members))
	for gsid, list := range g.members {
		for _, m := range list {
			out[gsid] = append(out[gsid], lastSeg(m.GetUser()))
		}
	}
	return out
}

// watchSessions reports the online sessions to rewards under metric. It returns a stop
// function; with rewards unconfigured, or no metric, it does nothing.
func (g *sessionServer) watchSessions(rw *emit.Client, metric string, min, every time.Duration) (stop func()) {
	if !rw.Enabled() || metric == "" {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		clock := newSessionClock(min)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			for _, p := range clock.tick(time.Now(), g.sessionSnapshot()) {
				baas := baasOf(p.uid)
				if baas == "" {
					log.Printf("[Rewards] no identity for a session player yet; retrying next tick")
					delete(clock.emitted, p.gsid+"/"+p.uid) // not reported: ask again
					continue
				}
				rw.Event(emit.Event{
					ID: fmt.Sprintf("%s:%s:%s", metric, p.gsid, baas), Metric: metric,
					Player: &emit.Identity{Namespace: "switch", Subject: baas}, Keys: []string{p.gsid},
				})
			}
			select {
			case <-t.C:
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}
