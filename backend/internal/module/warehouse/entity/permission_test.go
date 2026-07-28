package entity_test

// This file is the drift guard between the warehouse module's permission
// constants and the RBAC catalogue.
//
// It is the ONLY place the warehouse module references RBAC, and it does so
// from a _test package. ModuleConvention §6 forbids a module importing
// another's entity package in the production build graph; a test-only import
// does not appear there, so the architectural rule holds where it matters while
// the coupling is still verified.
//
// Without this, adding a code to one list and forgetting the other would
// produce a permission that can never be granted — and nothing else in the
// system would fail.

import (
	"testing"

	rbacentity "github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/entity"
)

func TestWarehousePermissionsMatchRBACCatalogue(t *testing.T) {
	catalogue := make(map[string]struct{}, len(rbacentity.PermissionCatalogue()))
	for _, code := range rbacentity.PermissionCatalogue() {
		catalogue[code.String()] = struct{}{}
	}

	for _, code := range entity.Permissions() {
		if _, ok := catalogue[code]; !ok {
			t.Errorf("warehouse declares %q but the RBAC catalogue does not; "+
				"it can never be granted", code)
		}
	}
}

// TestRBACWarehouseCodesAreDeclaredHere is the reverse direction: a code RBAC
// knows about but the warehouse module does not is one no route can enforce.
func TestRBACWarehouseCodesAreDeclaredHere(t *testing.T) {
	declared := make(map[string]struct{}, len(entity.Permissions()))
	for _, code := range entity.Permissions() {
		declared[code] = struct{}{}
	}

	const prefix = "warehouse."

	for _, code := range rbacentity.PermissionCatalogue() {
		value := code.String()
		if len(value) < len(prefix) || value[:len(prefix)] != prefix {
			continue
		}
		if _, ok := declared[value]; !ok {
			t.Errorf("the RBAC catalogue has %q but the warehouse module does not "+
				"declare it; no route can enforce it", value)
		}
	}
}

// TestPermissionValuesAreStable pins the exact strings.
//
// These are written into role_permissions rows by migration and into every
// grant an administrator makes. Renaming one silently revokes access everywhere
// it was granted, with no error anywhere — so the constants are asserted
// literally rather than compared to themselves.
func TestPermissionValuesAreStable(t *testing.T) {
	expected := map[string]string{
		"read":     "warehouse.read",
		"create":   "warehouse.create",
		"update":   "warehouse.update",
		"delete":   "warehouse.delete",
		"activate": "warehouse.activate",
	}

	actual := map[string]string{
		"read":     entity.PermissionRead,
		"create":   entity.PermissionCreate,
		"update":   entity.PermissionUpdate,
		"delete":   entity.PermissionDelete,
		"activate": entity.PermissionActivate,
	}

	for name, want := range expected {
		if actual[name] != want {
			t.Errorf("%s = %q, want %q — renaming a released code revokes access silently",
				name, actual[name], want)
		}
	}
}
