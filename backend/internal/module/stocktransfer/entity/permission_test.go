package entity_test

// Drift guard between the stock-transfer module's permission constants and the
// RBAC catalogue. The only place this module references RBAC, and it does so from
// a _test package so the production build graph stays clean (ModuleConvention §6).

import (
	"strings"
	"testing"

	rbacentity "github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/stocktransfer/entity"
)

func TestStockTransferPermissionsMatchRBACCatalogue(t *testing.T) {
	catalogue := make(map[string]struct{}, len(rbacentity.PermissionCatalogue()))
	for _, code := range rbacentity.PermissionCatalogue() {
		catalogue[code.String()] = struct{}{}
	}
	for _, code := range entity.Permissions() {
		if _, ok := catalogue[code]; !ok {
			t.Errorf("stocktransfer declares %q but the RBAC catalogue does not; it can never be granted", code)
		}
	}
}

func TestRBACStockTransferCodesAreDeclaredHere(t *testing.T) {
	declared := make(map[string]struct{}, len(entity.Permissions()))
	for _, code := range entity.Permissions() {
		declared[code] = struct{}{}
	}
	const prefix = "stocktransfer."
	for _, code := range rbacentity.PermissionCatalogue() {
		value := code.String()
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		if _, ok := declared[value]; !ok {
			t.Errorf("the RBAC catalogue has %q but the stocktransfer module does not declare it; no route can enforce it", value)
		}
	}
}

func TestPermissionValuesAreStable(t *testing.T) {
	expected := map[string]string{
		"read":     "stocktransfer.read",
		"create":   "stocktransfer.create",
		"update":   "stocktransfer.update",
		"confirm":  "stocktransfer.confirm",
		"complete": "stocktransfer.complete",
		"cancel":   "stocktransfer.cancel",
	}
	actual := map[string]string{
		"read":     entity.PermissionRead,
		"create":   entity.PermissionCreate,
		"update":   entity.PermissionUpdate,
		"confirm":  entity.PermissionConfirm,
		"complete": entity.PermissionComplete,
		"cancel":   entity.PermissionCancel,
	}
	for name, want := range expected {
		if actual[name] != want {
			t.Errorf("%s = %q, want %q — renaming a released code revokes access silently", name, actual[name], want)
		}
	}
}
