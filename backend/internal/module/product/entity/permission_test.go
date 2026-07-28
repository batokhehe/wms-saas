package entity_test

// This file is the drift guard between the product module's permission constants
// and the RBAC catalogue.
//
// It is the ONLY place the product module references RBAC, and it does so from a
// _test package. ModuleConvention §6 forbids a module importing another's entity
// package in the production build graph; a test-only import does not appear
// there, so the architectural rule holds where it matters while the coupling is
// still verified.
//
// Without this, adding a code to one list and forgetting the other would produce
// a permission that can never be granted — and nothing else would fail.

import (
	"testing"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	rbacentity "github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
)

func TestProductPermissionsMatchRBACCatalogue(t *testing.T) {
	catalogue := make(map[string]struct{}, len(rbacentity.PermissionCatalogue()))
	for _, code := range rbacentity.PermissionCatalogue() {
		catalogue[code.String()] = struct{}{}
	}

	for _, code := range entity.Permissions() {
		if _, ok := catalogue[code]; !ok {
			t.Errorf("product declares %q but the RBAC catalogue does not; "+
				"it can never be granted", code)
		}
	}
}

// TestRBACProductCodesAreDeclaredHere is the reverse direction: a code RBAC
// knows about but the product module does not is one no route can enforce.
func TestRBACProductCodesAreDeclaredHere(t *testing.T) {
	declared := make(map[string]struct{}, len(entity.Permissions()))
	for _, code := range entity.Permissions() {
		declared[code] = struct{}{}
	}

	const prefix = "product."

	for _, code := range rbacentity.PermissionCatalogue() {
		value := code.String()
		if len(value) < len(prefix) || value[:len(prefix)] != prefix {
			continue
		}
		if _, ok := declared[value]; !ok {
			t.Errorf("the RBAC catalogue has %q but the product module does not "+
				"declare it; no route can enforce it", value)
		}
	}
}

// TestPermissionValuesAreStable pins the exact strings.
//
// These are written into role_permissions rows by migration and into every grant
// an administrator makes. Renaming one silently revokes access everywhere it was
// granted, with no error anywhere — so the constants are asserted literally.
func TestPermissionValuesAreStable(t *testing.T) {
	expected := map[string]string{
		"read":        "product.read",
		"create":      "product.create",
		"update":      "product.update",
		"activate":    "product.activate",
		"discontinue": "product.discontinue",
	}

	actual := map[string]string{
		"read":        entity.PermissionRead,
		"create":      entity.PermissionCreate,
		"update":      entity.PermissionUpdate,
		"activate":    entity.PermissionActivate,
		"discontinue": entity.PermissionDiscontinue,
	}

	for name, want := range expected {
		if actual[name] != want {
			t.Errorf("%s = %q, want %q — renaming a released code revokes access silently",
				name, actual[name], want)
		}
	}
}
