package service

import (
	"context"

	"github.com/google/uuid"
)

// This file declares the goods-receipt module's EXTERNAL COLLABORATORS: the
// narrow interfaces the application service needs other modules to satisfy.
//
// The service NEVER reaches into another module's repository. It declares what it
// needs here; bootstrap — the composition root — supplies implementations. Only
// the ANSWER crosses each boundary, never a foreign aggregate.
//
// Every default below is a NAMED type, never nil, so an unwired dependency cannot
// be mistaken for "the collaborator permits it".

// WarehouseVerifier confirms the receiving warehouse exists in the company.
type WarehouseVerifier interface {
	VerifyWarehouse(ctx context.Context, companyID, warehouseID uuid.UUID) error
}

// AcceptAnyWarehouse accepts every well-formed warehouse id.
type AcceptAnyWarehouse struct{}

var _ WarehouseVerifier = (*AcceptAnyWarehouse)(nil)

// NewAcceptAnyWarehouse builds the permissive verifier.
func NewAcceptAnyWarehouse() *AcceptAnyWarehouse { return &AcceptAnyWarehouse{} }

// VerifyWarehouse always accepts.
func (AcceptAnyWarehouse) VerifyWarehouse(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// LocationVerifier confirms a receiving bin exists in the company AND belongs to
// the receiving warehouse. It takes the warehouse id as well precisely so that
// relationship can be checked, not merely existence.
type LocationVerifier interface {
	VerifyLocation(ctx context.Context, companyID, warehouseID, locationID uuid.UUID) error
}

// AcceptAnyLocation accepts every well-formed location id.
type AcceptAnyLocation struct{}

var _ LocationVerifier = (*AcceptAnyLocation)(nil)

// NewAcceptAnyLocation builds the permissive verifier.
func NewAcceptAnyLocation() *AcceptAnyLocation { return &AcceptAnyLocation{} }

// VerifyLocation always accepts.
func (AcceptAnyLocation) VerifyLocation(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}

// ProductVerifier confirms a received product exists in the company.
type ProductVerifier interface {
	VerifyProduct(ctx context.Context, companyID, productID uuid.UUID) error
}

// AcceptAnyProduct accepts every well-formed product id.
type AcceptAnyProduct struct{}

var _ ProductVerifier = (*AcceptAnyProduct)(nil)

// NewAcceptAnyProduct builds the permissive verifier.
func NewAcceptAnyProduct() *AcceptAnyProduct { return &AcceptAnyProduct{} }

// VerifyProduct always accepts.
func (AcceptAnyProduct) VerifyProduct(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// ---------------------------------------------------------------------------
// StockPoster
// ---------------------------------------------------------------------------

// StockArrival is one unit-bearing movement to book into inventory.
//
// It is a PLAIN struct, not an inventory DTO: the goods-receipt module must not
// import the inventory module's transport contract (ModuleConvention §6). The
// bootstrap adapter translates this into whatever the Inventory service wants.
type StockArrival struct {
	WarehouseID uuid.UUID
	LocationID  uuid.UUID
	ProductID   uuid.UUID

	// Tracking is NONE, LOT or SERIAL — the inventory module's vocabulary for how
	// a position is individuated.
	Tracking     string
	LotNumber    string
	SerialNumber string

	Quantity int64
}

// StockPoster books received stock into inventory.
//
// It is called INSIDE the receipt's transaction, so a failure to post rolls the
// whole receipt back: a receipt marked RECEIVED whose stock never landed would be
// worse than no receipt at all.
type StockPoster interface {
	PostArrival(ctx context.Context, arrival StockArrival) error
}

// RefuseStockPosting is the default when no inventory adapter is wired.
//
// It REFUSES rather than silently succeeding. A permissive default here would let
// a receipt reach RECEIVED with no stock behind it, which is exactly the
// inconsistency the transaction is meant to prevent — so an unwired poster must
// fail loudly at the first attempt to receive.
type RefuseStockPosting struct{}

var _ StockPoster = (*RefuseStockPosting)(nil)

// NewRefuseStockPosting builds the refusing default.
func NewRefuseStockPosting() *RefuseStockPosting { return &RefuseStockPosting{} }

// PostArrival always refuses.
func (RefuseStockPosting) PostArrival(context.Context, StockArrival) error {
	return errStockPostingUnwired
}

// ---------------------------------------------------------------------------
// PurchaseOrderReceiver
// ---------------------------------------------------------------------------

// PurchaseOrderReceipt reports that a quantity of one product arrived against a
// purchase order.
//
// A PLAIN struct, not a purchase-order DTO: the goods-receipt module must not
// import the purchase-order module's transport contract (ModuleConvention §6).
// The bootstrap adapter translates it.
//
// It names a PRODUCT rather than a purchase-order line: a receipt records what
// physically arrived and has no knowledge of the planning document's row
// structure. Resolving product to line is the purchase-order module's job,
// because the rule that makes it unambiguous — at most one line per product — is
// that module's invariant.
type PurchaseOrderReceipt struct {
	OrderID   uuid.UUID
	ProductID uuid.UUID
	Quantity  int64
}

// PurchaseOrderReceiver books arrivals against the purchase order a receipt
// references.
//
// It is called INSIDE the receipt's transaction, so a failure to update the order
// rolls the whole receipt back — the document, the stock and the ledger entry
// together. A receipt that created inventory while leaving its order showing the
// full quantity outstanding would make the order permanently uncompletable.
type PurchaseOrderReceiver interface {
	RecordReceipt(ctx context.Context, receipt PurchaseOrderReceipt) error
}

// RefusePurchaseOrderUpdates is the default when no purchase-order adapter is
// wired.
//
// It REFUSES rather than silently succeeding, for the same reason
// RefuseStockPosting does: a permissive default would let a receipt against a
// purchase order commit without updating it, which is precisely the gap this
// integration closes. It only ever fires for a receipt that actually references
// an order — a manual receipt never reaches it.
type RefusePurchaseOrderUpdates struct{}

var _ PurchaseOrderReceiver = (*RefusePurchaseOrderUpdates)(nil)

// NewRefusePurchaseOrderUpdates builds the refusing default.
func NewRefusePurchaseOrderUpdates() *RefusePurchaseOrderUpdates {
	return &RefusePurchaseOrderUpdates{}
}

// RecordReceipt always refuses.
func (RefusePurchaseOrderUpdates) RecordReceipt(context.Context, PurchaseOrderReceipt) error {
	return errPurchaseOrderUpdatesUnwired
}
