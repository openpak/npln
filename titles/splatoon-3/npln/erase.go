package npln

// Account erasure (openpak/prds#120): what Splatoon 3 keeps for a player, removed by NPLN user id.
// Registered with the host; the host resolves the user ids from the core's switch links.
// Idempotent.

import (
	"context"
	"os"
	"strings"
	"sync"

	host "github.com/openpak/npln"
	"github.com/openpak/npln/internal/blob"
)

var ugcStores struct {
	sync.Mutex
	list []*ugcStore
}

func init() { host.RegisterEraser(eraseUsers) }

func eraseUsers(_ context.Context, uids []string) (int, error) {
	set := make(map[string]bool, len(uids))
	for _, u := range uids {
		set[u] = true
	}
	n := 0

	live.Lock()
	for u := range set {
		if _, ok := live.attrs[u]; ok {
			n++
		}
		delete(live.attrs, u)
	}
	live.Unlock()

	// Cloud save: the Postgres row and the legacy file (a file left behind would be imported again).
	if db := blob.Default(); db != nil {
		c, err := db.DeleteKeys("cloudsave", uids)
		n += c
		if err != nil {
			return n, err
		}
	}
	saves.mu.Lock()
	for _, u := range uids {
		if p, err := saves.path(u); err == nil {
			if err := os.Remove(p); err == nil {
				n++
			}
		}
	}
	saves.mu.Unlock()

	// Splatfest entries.
	c, err := Fests.eraseUsers(set)
	n += c
	if err != nil {
		return n, err
	}

	// User documents (…/users/<uid>/…). The store has no owner field; the path is the owner.
	ugcStores.Lock()
	list := append([]*ugcStore(nil), ugcStores.list...)
	ugcStores.Unlock()
	for _, u := range list {
		n += u.eraseUsers(set)
	}
	return n, nil
}

func (s *festStore) eraseUsers(set map[string]bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	n := 0
	for _, m := range s.ent {
		for u := range set {
			if _, ok := m[u]; ok {
				delete(m, u)
				n++
			}
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.saveLocked()
}

func (u *ugcStore) eraseUsers(set map[string]bool) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := 0
	for path := range u.docs {
		seg := strings.Split(path, "/")
		for i := 0; i+1 < len(seg); i++ {
			if seg[i] == "users" && set[seg[i+1]] {
				delete(u.docs, path)
				n++
				break
			}
		}
	}
	if n > 0 {
		u.save()
	}
	return n
}
