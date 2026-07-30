package entity_test

// Drift guard between the purchase-order module's permission constants and the
// RBAC catalogue. The only place this module references RBAC, and it does so from
// a _test package so the production build graph stays clean (ModuleConvention §6).

import (
	"strings"
	"testing"

	purchaseorder "github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/entity"
	rbacentity "github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
)

func TestPurchaseOrderPermissionsMatchRBACCatalogue(t *testing.T) {
	catalogue := make(map[string]struct{}, len(rbacentity.PermissionCatalogue()))
	for _, code := range rbacentity.PermissionCatalogue() {
		catalogue[code.String()] = struct{}{}
	}
	for _, code := range purchaseorder.Permissions() {
		if _, ok := catalogue[code]; !ok {
			t.Errorf("purchaseorder declares %q but the RBAC catalogue does not; it can never be granted", code)
		}
	}
}

func TestRBACPurchaseOrderCodesAreDeclaredHere(t *testing.T) {
	declared := make(map[string]struct{}, len(purchaseorder.Permissions()))
	for _, code := range purchaseorder.Permissions() {
		declared[code] = struct{}{}
	}
	const prefix = "purchaseorder."
	for _, code := range rbacentity.PermissionCatalogue() {
		value := code.String()
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		if _, ok := declared[value]; !ok {
			t.Errorf("the RBAC catalogue has %q but the purchaseorder module does not declare it; no route can enforce it", value)
		}
	}
}

func TestPermissionValuesAreStable(t *testing.T) {
	expected := map[string]string{
		"read":    "purchaseorder.read",
		"create":  "purchaseorder.create",
		"update":  "purchaseorder.update",
		"approve": "purchaseorder.approve",
		"cancel":  "purchaseorder.cancel",
	}
	actual := map[string]string{
		"read":    purchaseorder.PermissionRead,
		"create":  purchaseorder.PermissionCreate,
		"update":  purchaseorder.PermissionUpdate,
		"approve": purchaseorder.PermissionApprove,
		"cancel":  purchaseorder.PermissionCancel,
	}
	for name, want := range expected {
		if actual[name] != want {
			t.Errorf("%s = %q, want %q — renaming a released code revokes access silently", name, actual[name], want)
		}
	}
}
