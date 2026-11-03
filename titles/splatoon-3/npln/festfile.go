package npln

// festfile — the operator's Splatfest definition and the stored team choices.
//
// fests.json (NPLN_FEST_FILE, default next to the rotation; re-read when it changes):
//
//	{"fests":[{
//	  "id": "JUEA-00201",                  // the fest id; resource names use it
//	  "regions": ["us","eu","jp","ap"],    // empty = every region
//	  "game_data": "juea-ability",         // FestResourceID of the BCAT pack
//	  "game_data_revision": "204e19c6...", // FestResourceRevision
//	  "teams": ["alpha","bravo","charlie"],// accepted team ids (empty = accept anything)
//	  "open":  "2026-11-07T00:00:00Z",     // served from here (announcement)
//	  "start": "2026-11-08T00:00:00Z",
//	  "mid":   "2026-11-08T12:00:00Z",
//	  "end":   "2026-11-09T00:00:00Z",
//	  "close": "2026-11-09T06:00:00Z"      // entries refused and schedule dropped after this
//	}]}
//
// Entries are kept in festentries.json beside it (one team per player and fest, first choice
// wins), rewritten atomically. A definition whose times are not strictly ordered
// open < start < mid < end < close is skipped with a log line, never served.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/openpak/npln/internal/blob"
	toyohrpb "github.com/openpak/npln/proto/toyohr/v1"
	"github.com/openpak/npln/redact"
)

type festState string

const (
	festScheduled festState = "scheduled" // before open: not served
	festAnnounced festState = "announced" // open..start
	festLive      festState = "live"      // start..end
	festFinal     festState = "final"     // end..close
	festClosed    festState = "closed"
)

type festDef struct {
	ID               string    `json:"id"`
	Regions          []string  `json:"regions"`
	GameData         string    `json:"game_data"`
	GameDataRevision string    `json:"game_data_revision"`
	Teams            []string  `json:"teams"`
	Open             time.Time `json:"open"`
	Start            time.Time `json:"start"`
	Mid              time.Time `json:"mid"`
	End              time.Time `json:"end"`
	Close            time.Time `json:"close"`
}

func (d *festDef) valid() error {
	if d.ID == "" || strings.Contains(d.ID, "/") {
		return fmt.Errorf("bad id %q", d.ID)
	}
	t := []time.Time{d.Open, d.Start, d.Mid, d.End, d.Close}
	for i := 1; i < len(t); i++ {
		if t[i-1].IsZero() || !t[i].After(t[i-1]) {
			return fmt.Errorf("times must be strictly ordered open < start < mid < end < close")
		}
	}
	return nil
}

func (d *festDef) state(now time.Time) festState {
	switch {
	case now.Before(d.Open):
		return festScheduled
	case now.Before(d.Start):
		return festAnnounced
	case now.Before(d.End):
		return festLive
	case now.Before(d.Close):
		return festFinal
	}
	return festClosed
}

func (d *festDef) hasTeam(t string) bool {
	if len(d.Teams) == 0 {
		return t != ""
	}
	for _, x := range d.Teams {
		if x == t {
			return true
		}
	}
	return false
}

func (d *festDef) inRegion(r string) bool {
	r = strings.ToLower(lastSeg(r))
	if len(d.Regions) == 0 || r == "" {
		return true
	}
	for _, x := range d.Regions {
		if strings.ToLower(x) == r {
			return true
		}
	}
	return false
}

func (d *festDef) schedule(tenant, target string) *toyohrpb.FestSchedule {
	pub := map[string]*toyohrpb.FestPublishment{}
	regions := d.Regions
	for _, r := range regions {
		pub[r] = &toyohrpb.FestPublishment{StartTime: timestamppb.New(d.Open), EndTime: timestamppb.New(d.Close)}
	}
	return &toyohrpb.FestSchedule{
		Name:                 tenant + "/festSchedules/" + d.ID,
		Fest:                 tenant + "/fests/" + d.ID,
		FestRegions:          regions,
		Target:               target,
		FestGameData:         d.GameData,
		FestGameDataRevision: d.GameDataRevision,
		Timetable: &toyohrpb.FestTimetable{OpenTime: timestamppb.New(d.Open), StartTime: timestamppb.New(d.Start),
			MidTime: timestamppb.New(d.Mid), EndTime: timestamppb.New(d.End), CloseTime: timestamppb.New(d.Close)},
		FestPublishments: pub,
	}
}

type festEntryRec struct {
	Team    string    `json:"team"`
	Region  string    `json:"region"`
	Created time.Time `json:"created"`
}

// festStore holds the definitions (reloaded on file change) and the entries.
type festStore struct {
	mu      sync.Mutex
	path    string
	entries string
	mtime   time.Time
	defs    []festDef
	ent     map[string]map[string]festEntryRec // fest id -> user id -> entry
	loaded  bool
}

// Fests is the process-wide store; the HTTP view (api) reads it too.
var Fests = &festStore{}

func (s *festStore) init() {
	if s.loaded {
		return
	}
	s.loaded = true
	rot := envOr("NPLN_ROTATION", "rotation.json")
	s.path = envOr("NPLN_FEST_FILE", filepath.Join(filepath.Dir(rot), "fests.json"))
	s.entries = envOr("NPLN_FEST_ENTRIES", filepath.Join(filepath.Dir(s.path), "festentries.json"))
	s.ent = map[string]map[string]festEntryRec{}
	var b []byte
	var err error
	if db := blob.Default(); db != nil {
		var ok bool
		if b, ok, err = db.Get("festentries", "all"); err == nil && !ok {
			if b, err = os.ReadFile(s.entries); err == nil { // import the old file, kept in place
				_, err = db.Import("festentries", "all", b)
			}
		}
	} else {
		b, err = os.ReadFile(s.entries)
	}
	if err == nil {
		if err := json.Unmarshal(b, &s.ent); err != nil {
			log.Printf("[Fest] entries: %s", redact.Err(err))
		}
	}
}

// refresh re-reads the definition file when it changed. Caller holds the lock.
func (s *festStore) refresh() {
	s.init()
	st, err := os.Stat(s.path)
	if err != nil {
		if s.defs != nil {
			log.Printf("[Fest] %s gone: no Splatfest defined", s.path)
		}
		s.defs, s.mtime = nil, time.Time{}
		return
	}
	if st.ModTime().Equal(s.mtime) {
		return
	}
	s.mtime = st.ModTime()
	s.defs = nil
	b, err := os.ReadFile(s.path)
	var f struct {
		Fests []festDef `json:"fests"`
	}
	if err == nil {
		err = json.Unmarshal(b, &f)
	}
	if err != nil {
		log.Printf("[Fest] %s: %s", s.path, redact.Err(err))
		return
	}
	for _, d := range f.Fests {
		if err := d.valid(); err != nil {
			log.Printf("[Fest] %s: fest %q skipped: %v", s.path, d.ID, err)
			continue
		}
		s.defs = append(s.defs, d)
	}
	log.Printf("[Fest] %s: %d fest(s) defined", s.path, len(s.defs))
}

// active is the fest to announce at now for a region: the earliest-ending one that is open and
// not closed.
func (s *festStore) active(now time.Time, region string) *festDef {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	var best *festDef
	for i := range s.defs {
		d := &s.defs[i]
		if st := d.state(now); st == festScheduled || st == festClosed || !d.inRegion(region) {
			continue
		}
		if best == nil || d.End.Before(best.End) {
			best = d
		}
	}
	return best
}

func (s *festStore) byID(id string) *festDef {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	for i := range s.defs {
		if s.defs[i].ID == id {
			d := s.defs[i]
			return &d
		}
	}
	return nil
}

func (s *festStore) entry(fest, uid string) (festEntryRec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	e, ok := s.ent[fest][uid]
	return e, ok
}

// enter stores the player's team for a fest; a second call returns the first choice.
func (s *festStore) enter(fest, uid, team, region string) (festEntryRec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	if e, ok := s.ent[fest][uid]; ok {
		return e, nil
	}
	e := festEntryRec{Team: team, Region: region, Created: time.Now().UTC()}
	if s.ent[fest] == nil {
		s.ent[fest] = map[string]festEntryRec{}
	}
	s.ent[fest][uid] = e
	b, _ := json.MarshalIndent(s.ent, "", " ")
	var tmp *os.File
	var err error
	if db := blob.Default(); db != nil {
		err = db.Put("festentries", "all", b)
	} else {
		tmp, err = os.CreateTemp(filepath.Dir(s.entries), ".festentries-*.tmp")
	}
	if tmp != nil && err == nil {
		_, err = tmp.Write(b)
		if cerr := tmp.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(tmp.Name(), s.entries)
		} else {
			os.Remove(tmp.Name())
		}
	}
	if err != nil {
		delete(s.ent[fest], uid) // not durable: do not claim it
		return festEntryRec{}, err
	}
	return e, nil
}

func entryMsg(tenant, fest, uid string, e festEntryRec) *toyohrpb.FestEntry {
	return &toyohrpb.FestEntry{Name: tenant + "/fests/" + fest + "/entries/" + uid, FestTeam: e.Team, FestRegion: e.Region}
}

// festOfEntryName pulls {f} out of "tenants/{t}/fests/{f}/entries/{e}".
func festOfEntryName(name string) string {
	p := strings.Split(name, "/")
	for i := 0; i+1 < len(p); i++ {
		if p[i] == "fests" {
			return p[i+1]
		}
	}
	return ""
}

// ---- JSON view for the site ----

type FestView struct {
	ID               string               `json:"id"`
	State            festState            `json:"state"`
	Regions          []string             `json:"regions"`
	GameData         string               `json:"game_data"`
	GameDataRevision string               `json:"game_data_revision"`
	Teams            []string             `json:"teams"`
	Times            map[string]time.Time `json:"times"`
	Entries          map[string]int       `json:"entries_per_team"`
}

// View lists every defined fest with its state at now and the number of entries per team.
func (s *festStore) View(now time.Time) []FestView {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refresh()
	out := []FestView{}
	for _, d := range s.defs {
		v := FestView{ID: d.ID, State: d.state(now), Regions: d.Regions, GameData: d.GameData, GameDataRevision: d.GameDataRevision,
			Teams: d.Teams, Entries: map[string]int{},
			Times: map[string]time.Time{"open": d.Open, "start": d.Start, "mid": d.Mid, "end": d.End, "close": d.Close}}
		for _, e := range s.ent[d.ID] {
			v.Entries[e.Team]++
		}
		out = append(out, v)
	}
	return out
}
