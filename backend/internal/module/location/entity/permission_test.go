package entity_test

// This file is the drift guard between the location module's permission
// constants and the RBAC catalogue.
//
// It is the ONLY place the location module references RBAC, and it does so from
// a _test package. ModuleConvention §6 forbids a module importing another's
// entity package in the production build graph; a test-only import does not
// appear there, so the architectural rule holds where it matters while the
// coupling is still verified.
//
// Without this, adding a code to one list and forgetting the other would
// produce a permission that can never be granted — and nothing else in the
// system would fail.

import (
	"strings"
	"testing"

	"github.com/batokhehe/wms-saas/backend/internal/module/location/entity"
	rbacentity "github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
)

func TestLocationPermissionsMatchRBACCatalogue(t *testing.T) {
	catalogue := make(map[string]struct{}, len(rbacentity.PermissionCatalogue()))
	for _, code := range rbacentity.PermissionCatalogue() {
		catalogue[code.String()] = struct{}{}
	}

	for _, code := range entity.Permissions() {
		if _, ok := catalogue[code]; !ok {
			t.Errorf("location declares %q but the RBAC catalogue does not; "+
				"it can never be granted", code)
		}
	}
}

// TestRBACLocationCodesAreDeclaredHere is the reverse direction: a code RBAC
// knows about but the location module does not is one no route can enforce.
func TestRBACLocationCodesAreDeclaredHere(t *testing.T) {
	declared := make(map[string]struct{}, len(entity.Permissions()))
	for _, code := range entity.Permissions() {
		declared[code] = struct{}{}
	}

	for _, code := range rbacentity.PermissionCatalogue() {
		value := code.String()
		if !strings.HasPrefix(value, "location.") {
			continue
		}
		if _, ok := declared[value]; !ok {
			t.Errorf("the RBAC catalogue has %q but the location module does not "+
				"declare it; no route can enforce it", value)
		}
	}
}

// TestPermissionValuesAreStable pins the exact strings.
//
// These are written into role_permissions rows by migration and into every
// grant an administrator makes. Renaming one silently revokes access everywhere
// it was granted, with no error anywhere.
func TestPermissionValuesAreStable(t *testing.T) {
	expected := map[string]string{
		"read":   "location.read",
		"create": "location.create",
		"update": "location.update",
		"lock":   "location.lock",
	}

	actual := map[string]string{
		"read":   entity.PermissionRead,
		"create": entity.PermissionCreate,
		"update": entity.PermissionUpdate,
		"lock":   entity.PermissionLock,
	}

	for name, want := range expected {
		if actual[name] != want {
			t.Errorf("%s = %q, want %q — renaming a released code revokes access silently",
				name, actual[name], want)
		}
	}
}
