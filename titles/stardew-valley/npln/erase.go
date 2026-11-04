package npln

// Account erasure (openpak/prds#120): what this package keeps for a player, removed by NPLN user
// id. Registered with the host (host.RegisterEraser); the host resolves the account's user ids
// from the core's switch links. Idempotent: a second run finds nothing.

import (
	"context"
	"strings"
	"sync"

	host "github.com/openpak/npln"
)

// instances are the live servers of this process, so an erase reaches their in-memory state.
var instances struct {
	sync.Mutex
	rooms  []*sessionServer
	msgs   []*messaging
	mms    []*matchmaker
	ugcs   []*ugcStore
	hydros []*hydro
}

func trackRooms(r *sessionServer) {
	instances.Lock()
	instances.rooms = append(instances.rooms, r)
	instances.Unlock()
}

func trackedMatchmaker(rooms *sessionServer) *matchmaker {
	m := newMatchmaker(rooms)
	instances.Lock()
	instances.mms = append(instances.mms, m)
	instances.Unlock()
	return m
}

func trackedMessaging(rooms *sessionServer) *messaging {
	m := newMessaging(rooms)
	instances.Lock()
	instances.msgs = append(instances.msgs, m)
	instances.Unlock()
	return m
}

func trackedUGC(u *ugcStore) *ugcStore {
	instances.Lock()
	instances.ugcs = append(instances.ugcs, u)
	instances.Unlock()
	return u
}

func trackedHydro(h *hydro) *hydro {
	instances.Lock()
	instances.hydros = append(instances.hydros, h)
	instances.Unlock()
	return h
}

func init() { host.RegisterEraser(eraseUsers) }

func eraseUsers(_ context.Context, uids []string) (int, error) {
	set := make(map[string]bool, len(uids))
	for _, u := range uids {
		set[u] = true
	}
	n := 0

	// Process-wide maps.
	players.Lock()
	for u := range set {
		if _, ok := players.baas[u]; ok {
			n++
		}
		delete(players.baas, u)
		delete(players.pid, u)
	}
	players.Unlock()
	for u := range set {
		if _, ok := playerByUID.LoadAndDelete(u); ok {
			n++
		}
		if _, ok := appVersionByUID.LoadAndDelete(u); ok {
			n++
		}
	}
	live.Lock()
	for u := range set {
		if _, ok := live.attrs[u]; ok {
			n++
		}
		delete(live.attrs, u)
	}
	live.Unlock()

	instances.Lock()
	rooms := append([]*sessionServer(nil), instances.rooms...)
	msgs := append([]*messaging(nil), instances.msgs...)
	mms := append([]*matchmaker(nil), instances.mms...)
	ugcs := append([]*ugcStore(nil), instances.ugcs...)
	hydros := append([]*hydro(nil), instances.hydros...)
	instances.Unlock()

	for _, r := range rooms {
		r.mu.Lock()
		for gsid, list := range r.members {
			kept := list[:0]
			for _, m := range list {
				if set[lastSeg(m.GetUser())] {
					n++
					continue
				}
				kept = append(kept, m)
			}
			r.members[gsid] = kept
		}
		r.mu.Unlock()
	}
	for _, m := range msgs {
		m.mu.Lock()
		for u := range set {
			if b, ok := m.boxes[u]; ok {
				n += len(b.items) + 1
				for _, q := range b.items {
					m.drop(u, q)
				}
				delete(m.boxes, u)
			}
		}
		for uid, b := range m.boxes {
			kept := b.items[:0]
			for _, q := range b.items {
				if set[q.from] {
					n++
					m.drop(uid, q)
					continue
				}
				kept = append(kept, q)
			}
			b.items = kept
		}
		m.mu.Unlock()
	}
	for _, m := range mms {
		m.mu.Lock()
		for id, t := range m.tickets {
			if set[t.owner] {
				delete(m.tickets, id)
				n++
			}
		}
		m.mu.Unlock()
	}
	for _, h := range hydros {
		h.mu.Lock()
		for name, c := range h.contents {
			if set[lastSeg(c.GetUser())] {
				delete(h.contents, name)
				delete(h.rev, name)
				h.store(name)
				n++
			}
		}
		h.mu.Unlock()
	}
	for _, u := range ugcs {
		n += u.eraseUsers(set)
	}

	// Durable: counted reward sessions ("<gsid>/<uid>" keys), in Postgres or the state file.
	if st := openCounted(); st != nil {
		c, err := st.Forget(uids)
		st.Close()
		n += c
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// eraseUsers deletes the documents the players own or whose path names them, and their short
// codes, then saves once. Opaque document bodies cannot be scrubbed of a player, so owned
// documents go rather than being kept under a placeholder owner.
func (u *ugcStore) eraseUsers(set map[string]bool) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := 0
	for path, e := range u.docs {
		drop := set[e.owner]
		for _, s := range strings.Split(path, "/") {
			if set[s] {
				drop = true
			}
		}
		if drop {
			delete(u.docs, path)
			n++
		}
	}
	for code, a := range u.aliases {
		if set[a.Owner] || u.docs[a.Doc] == nil {
			delete(u.aliases, code)
			n++
		}
	}
	if n > 0 {
		u.save()
	}
	return n
}
