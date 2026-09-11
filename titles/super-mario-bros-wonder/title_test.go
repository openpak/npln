package wonder

import "testing"

func TestTenantDefaultsAndShape(t *testing.T) {
	t.Setenv("NPLN_TENANT", "")
	if got, err := Tenant(); err != nil || got != DefaultTenant {
		t.Fatalf("default: %q, %v", got, err)
	}
	t.Setenv("NPLN_TENANT", "t-0badf00d-lp1")
	if _, err := Tenant(); err == nil {
		t.Fatal("bare label accepted; the resource prefix is tenants/…")
	}
	t.Setenv("NPLN_TENANT", "tenants/t-0badf00d-lp1")
	if got, err := Tenant(); err != nil || got != "tenants/t-0badf00d-lp1" {
		t.Fatalf("override: %q, %v", got, err)
	}
}
