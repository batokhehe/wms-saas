package service

import (
	"context"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/dto"
)

// This file declares the INTEGRATION SEAM between the Inventory module and the
// ledger. It is interfaces only — there is no event bus here, and none is
// implied.
//
// # Why a seam rather than a direct call
//
// The ledger must witness every stock transition, but Inventory must not depend
// on the ledger to do its job: stock has to keep moving whether or not an audit
// consumer exists, and a position is the source of truth regardless. Expressing
// the relationship as two narrow interfaces means the wiring decision — call it
// inline, queue it, fan it out to several consumers — is made once at the
// composition root and changes neither module.
//
// # How the two fit together
//
//	Inventory  --publishes-->  InventoryEventPublisher
//	                                   |
//	                            (composition root)
//	                                   |
//	InventoryEventSubscriber  <--delivers--  ledger.Service
//
// The ledger's Service already satisfies InventoryEventSubscriber, so the
// simplest wiring is to hand the publisher a direct reference. A later sprint can
// put a queue in between without either side noticing.

// InventoryEventPublisher is what the INVENTORY module would call to announce a
// stock transition. The ledger declares it — rather than Inventory — so the shape
// of an announcement is defined by the side that has to consume it.
//
// The delivery mechanism is now chosen and wired: bootstrap adapts the Inventory
// module's LedgerPublisher onto InventoryEventSubscriber below, so a movement
// calls straight through to this service. The call is SYNCHRONOUS and its error
// is propagated, which is what makes the entry land in the movement's own
// transaction — see OnInventoryMovement.
type InventoryEventPublisher interface {
	// PublishMovement announces that a position changed.
	//
	// It returns an error so a synchronous implementation can make the caller's
	// transaction fail when the ledger cannot record the movement. An
	// asynchronous implementation would return nil once the message is durably
	// enqueued — the choice belongs to the implementation, not the contract.
	PublishMovement(ctx context.Context, movement dto.RecordMovementRequest) error
}

// InventoryEventSubscriber is what receives an announced transition. The ledger's
// Service implements it.
//
// It takes the same payload as the publisher, so a direct wiring is a single
// assignment with no adapter in between.
type InventoryEventSubscriber interface {
	// OnInventoryMovement handles one announced transition.
	//
	// An implementation must be IDEMPOTENT-SAFE in the sense that a duplicate
	// delivery is refused rather than silently duplicating history: the ledger
	// rejects a repeated entry id with a CONFLICT instead of overwriting.
	//
	// It must also JOIN the caller's transaction rather than opening its own, and
	// must return its error rather than swallowing it. Both together are what
	// guarantee a position never moves without an entry and an entry never
	// describes a movement that rolled back.
	OnInventoryMovement(ctx context.Context, movement dto.RecordMovementRequest) error
}
