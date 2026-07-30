package bootstrap

import (
	"context"

	inventoryservice "github.com/batokhehe/wms-saas/backend/internal/module/inventory/service"
	ledgerdto "github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/dto"
	ledgerservice "github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/service"
)

// This file closes the Inventory -> Inventory Ledger integration.
//
// The two modules were built against each other's declared seams and never
// connected: Inventory declares LedgerPublisher (what it needs called), the
// ledger declares InventoryEventSubscriber (what it can receive). Both have the
// same payload — inventoryledger/dto.RecordMovementRequest — so the adapter is a
// pure rename, and bootstrap is the only place that has to know both names.
//
// # Why this is synchronous
//
// The call happens on the inventory movement's own goroutine and connection, and
// its error is propagated. That is deliberate and it is the entire guarantee: the
// ledger service joins the movement's transaction through a SAVEPOINT, so the
// position row and the entry that witnesses it commit together or not at all.
// Queueing the entry instead would break it — the movement would commit and the
// audit record would be a promise, not a fact.

type inventoryLedgerPublisher struct {
	subscriber ledgerservice.InventoryEventSubscriber
}

var _ inventoryservice.LedgerPublisher = (*inventoryLedgerPublisher)(nil)

func newInventoryLedgerPublisher(subscriber ledgerservice.InventoryEventSubscriber) *inventoryLedgerPublisher {
	return &inventoryLedgerPublisher{subscriber: subscriber}
}

// RecordMovement delivers one movement to the ledger, returning its error so the
// caller's transaction fails when the entry cannot be written.
func (p *inventoryLedgerPublisher) RecordMovement(
	ctx context.Context, movement ledgerdto.RecordMovementRequest,
) error {
	return p.subscriber.OnInventoryMovement(ctx, movement)
}
