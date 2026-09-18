package stardewset

import "testing"

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
