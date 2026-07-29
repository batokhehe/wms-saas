package entity_test

// Drift guard between the module's permission constant and the RBAC catalogue.
// The only place this module references RBAC, and from a _test package so the
// production build graph stays clean.

import (
	"testing"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/entity"
	rbacentity "github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
)

func TestPermissionsMatchRBACCatalogue(t *testing.T) {
	catalogue := make(map[string]struct{}, len(rbacentity.PermissionCatalogue()))
	for _, code := range rbacentity.PermissionCatalogue() {
		catalogue[code.String()] = struct{}{}
	}
	for _, code := range entity.Permissions() {
		if _, ok := catalogue[code]; !ok {
			t.Errorf("inventoryledger declares %q but the RBAC catalogue does not; it can never be granted", code)
		}
	}
}

func TestRBACLedgerCodesAreDeclaredHere(t *testing.T) {
	declared := make(map[string]struct{}, len(entity.Permissions()))
	for _, code := range entity.Permissions() {
		declared[code] = struct{}{}
	}
	const prefix = "inventoryledger."
	for _, code := range rbacentity.PermissionCatalogue() {
		value := code.String()
		if len(value) < len(prefix) || value[:len(prefix)] != prefix {
			continue
		}
		if _, ok := declared[value]; !ok {
			t.Errorf("the RBAC catalogue has %q but the module does not declare it; no route can enforce it", value)
		}
	}
}

// TestLedgerGrantsNoWriteCapability pins the read-only design: if a write code is
// ever added, this fails and forces the author to justify an operation the module
// deliberately does not offer.
func TestLedgerGrantsNoWriteCapability(t *testing.T) {
	if got := entity.Permissions(); len(got) != 1 || got[0] != "inventoryledger.read" {
		t.Fatalf("permissions = %v, want exactly [inventoryledger.read]", got)
	}
}
