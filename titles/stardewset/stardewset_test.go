package stardewset

import (
	"testing"

	"github.com/openpak/npln/titles/stardew-valley/npln"
)

func TestTenantDefaultsAndShape(t *testing.T) {
	const def = "tenants/t-ba973ec6-lp1"
	t.Setenv("NPLN_TENANT", "")
	if got, err := Tenant(def); err != nil || got != def {
		t.Fatalf("default: %q, %v", got, err)
	}
	if _, err := Tenant(""); err == nil {
		t.Fatal("started with no tenant at all")
	}
	t.Setenv("NPLN_TENANT", "t-0badf00d-lp1")
	if _, err := Tenant(def); err == nil {
		t.Fatal("bare label accepted; the resource prefix is tenants/…")
	}
	t.Setenv("NPLN_TENANT", "tenants/t-0badf00d-lp1")
	if got, err := Tenant(""); err != nil || got != "tenants/t-0badf00d-lp1" {
		t.Fatalf("override: %q, %v", got, err)
	}
}

func TestWithRewardsPerTitleFamily(t *testing.T) {
	t.Setenv("REWARDS_EVENTS_URL", "http://127.0.0.1:1")
	t.Setenv("REWARDS_EVENTS_KEY", "k")
	// plain set (Terraria, PlateUp!, Human: Fall Flat ...), set with options (Wonder, Core Keeper),
	// and a title that already chose its metric.
	plain := withRewards("plateup", nil)[0]
	if plain.RewardsMetric != "game_sessions.plateup" || !plain.Rewards.Enabled() {
		t.Fatalf("plain: %q enabled=%v", plain.RewardsMetric, plain.Rewards.Enabled())
	}
	opt := withRewards("super-mario-bros-wonder", []npln.Options{{Matchmaker: true, ConfigProperties: true}})[0]
	if opt.RewardsMetric != "game_sessions.super-mario-bros-wonder" || !opt.Matchmaker || !opt.ConfigProperties {
		t.Fatalf("options lost or metric wrong: %+v", opt)
	}
	own := withRewards("x", []npln.Options{{RewardsMetric: "custom.x"}})[0]
	if own.RewardsMetric != "custom.x" {
		t.Fatalf("own metric overridden: %q", own.RewardsMetric)
	}
}
