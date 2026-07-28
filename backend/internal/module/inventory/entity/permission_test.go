package entity_test

// This file is the drift guard between the inventory module's permission
// constants and the RBAC catalogue.
//
// It is the ONLY place the inventory module references RBAC, and it does so from
// a _test package. ModuleConvention §6 forbids a module importing another's
// entity package in the production build graph; a test-only import does not
// appear there, so the architectural rule holds where it matters while the
// coupling is still verified.

import (
	"testing"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	rbacentity "github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
)

func TestInventoryPermissionsMatchRBACCatalogue(t *testing.T) {
	catalogue := make(map[string]struct{}, len(rbacentity.PermissionCatalogue()))
	for _, code := range rbacentity.PermissionCatalogue() {
		catalogue[code.String()] = struct{}{}
	}
	for _, code := range entity.Permissions() {
		if _, ok := catalogue[code]; !ok {
			t.Errorf("inventory declares %q but the RBAC catalogue does not; it can never be granted", code)
		}
	}
}

// TestRBACInventoryCodesAreDeclaredHere is the reverse direction: a code RBAC
// knows about but the inventory module does not is one no route can enforce.
func TestRBACInventoryCodesAreDeclaredHere(t *testing.T) {
	declared := make(map[string]struct{}, len(entity.Permissions()))
	for _, code := range entity.Permissions() {
		declared[code] = struct{}{}
	}

	const prefix = "inventory."

	for _, code := range rbacentity.PermissionCatalogue() {
		value := code.String()
		if len(value) < len(prefix) || value[:len(prefix)] != prefix {
			continue
		}
		if _, ok := declared[value]; !ok {
			t.Errorf("the RBAC catalogue has %q but the inventory module does not declare it; no route can enforce it", value)
		}
	}
}

// TestPermissionValuesAreStable pins the exact strings. Renaming a released code
// silently revokes access everywhere it was granted, with no error anywhere.
func TestPermissionValuesAreStable(t *testing.T) {
	expected := map[string]string{
		"read":       "inventory.read",
		"create":     "inventory.create",
		"update":     "inventory.update",
		"adjust":     "inventory.adjust",
		"reserve":    "inventory.reserve",
		"transfer":   "inventory.transfer",
		"lock":       "inventory.lock",
		"cyclecount": "inventory.cyclecount",
	}
	actual := map[string]string{
		"read":       entity.PermissionRead,
		"create":     entity.PermissionCreate,
		"update":     entity.PermissionUpdate,
		"adjust":     entity.PermissionAdjust,
		"reserve":    entity.PermissionReserve,
		"transfer":   entity.PermissionTransfer,
		"lock":       entity.PermissionLock,
		"cyclecount": entity.PermissionCycleCount,
	}
	for name, want := range expected {
		if actual[name] != want {
			t.Errorf("%s = %q, want %q — renaming a released code revokes access silently", name, actual[name], want)
		}
	}
}
