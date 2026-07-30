package entity_test

// Drift guard between the goods-receipt module's permission constants and the
// RBAC catalogue. The only place this module references RBAC, from a _test
// package so the production build graph stays clean (ModuleConvention §6).

import (
	"strings"
	"testing"

	goodsreceipt "github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/entity"
	rbacentity "github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
)

func TestGoodsReceiptPermissionsMatchRBACCatalogue(t *testing.T) {
	catalogue := make(map[string]struct{}, len(rbacentity.PermissionCatalogue()))
	for _, code := range rbacentity.PermissionCatalogue() {
		catalogue[code.String()] = struct{}{}
	}
	for _, code := range goodsreceipt.Permissions() {
		if _, ok := catalogue[code]; !ok {
			t.Errorf("goodsreceipt declares %q but the RBAC catalogue does not; it can never be granted", code)
		}
	}
}

func TestRBACGoodsReceiptCodesAreDeclaredHere(t *testing.T) {
	declared := make(map[string]struct{}, len(goodsreceipt.Permissions()))
	for _, code := range goodsreceipt.Permissions() {
		declared[code] = struct{}{}
	}
	const prefix = "goodsreceipt."
	for _, code := range rbacentity.PermissionCatalogue() {
		value := code.String()
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		if _, ok := declared[value]; !ok {
			t.Errorf("the RBAC catalogue has %q but the goodsreceipt module does not declare it", value)
		}
	}
}

// TestUOMReadIsCatalogued is the Part 1 regression guard. uom.read is enforced by
// the /lookups/uoms route, and its absence from the catalogue made that endpoint
// answer 403 to every caller including OWNER.
func TestUOMReadIsCatalogued(t *testing.T) {
	for _, code := range rbacentity.PermissionCatalogue() {
		if code.String() == "uom.read" {
			return
		}
	}
	t.Fatal("uom.read is missing from the RBAC catalogue; /lookups/uoms cannot be granted")
}
