package inventory

import (
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	invservice "github.com/batokhehe/wms-saas/backend/internal/module/inventory/service"
	adapterclock "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/clock"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
)

// TestModuleWiring proves the module satisfies the platform contract and mounts
// its full route table. It constructs the module with no database — registration
// touches neither the repository nor the service — so a wiring mistake surfaces
// here rather than as a missing endpoint in production.
func TestModuleWiring(t *testing.T) {
	gin.SetMode(gin.TestMode)

	m := New(module.Dependencies{
		DB:     nil, // never dereferenced during construction or registration
		Logger: zap.NewNop(),
		Clock:  adapterclock.NewSystem(),
		IDs:    adapterid.NewUUID(),
	}, Config{
		Products:   invservice.NewAcceptAnyProduct(),
		Warehouses: invservice.NewAcceptAnyWarehouse(),
		Locations:  invservice.NewAcceptAnyLocation(),
		Policy:     invservice.NewDefaultStockPolicy(),
	})

	if m.Name() != "inventory" {
		t.Fatalf("Name() = %q, want inventory", m.Name())
	}

	engine := gin.New()
	m.RegisterV1(engine.Group("/api/v1"))

	got := map[string]bool{}
	for _, r := range engine.Routes() {
		got[r.Method+" "+r.Path] = true
	}

	want := []string{
		"GET /api/v1/inventory-positions",
		"GET /api/v1/inventory-positions/:id",
		"POST /api/v1/inventory-positions/receive",
		"POST /api/v1/inventory-positions/:id/issue",
		"POST /api/v1/inventory-positions/:id/reserve",
		"POST /api/v1/inventory-positions/:id/release",
		"POST /api/v1/inventory-positions/:id/allocate",
		"POST /api/v1/inventory-positions/:id/deallocate",
		"POST /api/v1/inventory-positions/:id/quarantine",
		"POST /api/v1/inventory-positions/:id/release-quarantine",
		"POST /api/v1/inventory-positions/:id/transfer",
		"POST /api/v1/inventory-positions/:id/adjust",
	}
	for _, route := range want {
		if !got[route] {
			t.Errorf("route not registered: %s", route)
		}
	}
	if len(engine.Routes()) != len(want) {
		t.Errorf("registered %d routes, want %d", len(engine.Routes()), len(want))
	}
}
