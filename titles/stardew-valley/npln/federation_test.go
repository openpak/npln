package npln

import "testing"

// A player in a country with a node gets its TURN; anyone else, and a node without one, fall
// back to this deployment's own.
func TestIceFollowsPlayerCountry(t *testing.T) {
	t.Setenv("NPLN_STUN_HOST", "stun.home.example")
	rs := []Region{
		{ID: "au-sydney", Country: "AU", Services: map[string]string{"turn": "203.0.113.9:3478"}},
		{ID: "nz-akl", Country: "NZ", Services: map[string]string{"photon": "x"}},
	}
	regions.Store(&rs)
	t.Cleanup(func() { regions.Store(nil) })
	countryByUID.Store("u-au", "AU")
	countryByUID.Store("u-nz", "NZ")

	if s, _, tu, tp := iceFor("u-au"); s != "203.0.113.9" || tu != "203.0.113.9" || tp != 3478 {
		t.Fatalf("au ice: stun=%s turn=%s:%d", s, tu, tp)
	}
	if s, _, tu, _ := iceFor("u-nz"); s != "stun.home.example" || tu != "stun.home.example" {
		t.Fatalf("nz ice, want home: stun=%s turn=%s", s, tu)
	}
	if s, _, _, _ := iceFor("u-unknown"); s != "stun.home.example" {
		t.Fatalf("unknown player, want home: %s", s)
	}
}
