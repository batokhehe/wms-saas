package entity_test

// Drift guard between the customer module's permission constants and the RBAC
// catalogue. The only place the customer module references RBAC, from a _test
// package so the production build graph stays clean.

import (
	"testing"

	"github.com/batokhehe/wms-saas/backend/internal/module/customer/entity"
	rbacentity "github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
)

func TestCustomerPermissionsMatchRBACCatalogue(t *testing.T) {
	catalogue := make(map[string]struct{}, len(rbacentity.PermissionCatalogue()))
	for _, code := range rbacentity.PermissionCatalogue() {
		catalogue[code.String()] = struct{}{}
	}
	for _, code := range entity.Permissions() {
		if _, ok := catalogue[code]; !ok {
			t.Errorf("customer declares %q but the RBAC catalogue does not; it can never be granted", code)
		}
	}
}

func TestRBACCustomerCodesAreDeclaredHere(t *testing.T) {
	declared := make(map[string]struct{}, len(entity.Permissions()))
	for _, code := range entity.Permissions() {
		declared[code] = struct{}{}
	}
	const prefix = "customer."
	for _, code := range rbacentity.PermissionCatalogue() {
		value := code.String()
		if len(value) < len(prefix) || value[:len(prefix)] != prefix {
			continue
		}
		if _, ok := declared[value]; !ok {
			t.Errorf("the RBAC catalogue has %q but the customer module does not declare it; no route can enforce it", value)
		}
	}
}

func TestPermissionValuesAreStable(t *testing.T) {
	expected := map[string]string{
		"read":     "customer.read",
		"create":   "customer.create",
		"update":   "customer.update",
		"activate": "customer.activate",
	}
	actual := map[string]string{
		"read":     entity.PermissionRead,
		"create":   entity.PermissionCreate,
		"update":   entity.PermissionUpdate,
		"activate": entity.PermissionActivate,
	}
	for name, want := range expected {
		if actual[name] != want {
			t.Errorf("%s = %q, want %q — renaming a released code revokes access silently", name, actual[name], want)
		}
	}
}
