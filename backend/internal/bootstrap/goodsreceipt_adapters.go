package bootstrap

import (
	"context"

	"github.com/google/uuid"

	goodsreceiptservice "github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/service"
	inventorydto "github.com/batokhehe/wms-saas/backend/internal/module/inventory/dto"
	inventoryservice "github.com/batokhehe/wms-saas/backend/internal/module/inventory/service"
	purchaseorderservice "github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/service"
)

// This file joins the goods-receipt module to inventory, without goodsreceipt
// importing the inventory module.
//
// GoodsReceipt declares a narrow StockPoster seam over a PLAIN struct
// (goodsreceipt/service.StockArrival); bootstrap — the composition root — is the
// only place that knows both vocabularies, and translates one into the other.
// That is what keeps ModuleConvention §6 intact while still letting a receipt
// create stock.
//
// The warehouse, location and product verifiers are NOT redeclared here: the
// adapters in inventory_adapters.go have identical method sets, and Go interfaces
// are structural, so the same instances satisfy the goods-receipt contracts too.

// ---------- goodsreceipt → inventory ----------

type goodsReceiptStockPoster struct {
	inventory *inventoryservice.Service
}

var _ goodsreceiptservice.StockPoster = (*goodsReceiptStockPoster)(nil)

func newGoodsReceiptStockPoster(inventory *inventoryservice.Service) *goodsReceiptStockPoster {
	return &goodsReceiptStockPoster{inventory: inventory}
}

// PostArrival books one arrival into inventory.
//
// It calls the inventory APPLICATION SERVICE rather than its repository, so every
// invariant the Inventory aggregate enforces — a serial position holding exactly
// one unit, buckets never going negative — applies to stock arriving from a
// receipt exactly as it does to stock received directly.
//
// The call runs inside the goods receipt's transaction. The shared transaction
// manager joins an existing transaction through a SAVEPOINT rather than opening a
// second one, so a failure here rolls the receipt back with it.
func (p *goodsReceiptStockPoster) PostArrival(
	ctx context.Context, arrival goodsreceiptservice.StockArrival,
) error {
	request := inventorydto.ReceiveStockRequest{
		StockKeyRequest: inventorydto.StockKeyRequest{
			WarehouseID: arrival.WarehouseID,
			LocationID:  arrival.LocationID,
			ProductID:   arrival.ProductID,
			Tracking:    arrival.Tracking,
		},
		Quantity: arrival.Quantity,
	}
	if arrival.LotNumber != "" {
		lot := arrival.LotNumber
		request.LotNumber = &lot
	}
	if arrival.SerialNumber != "" {
		serial := arrival.SerialNumber
		request.SerialNumber = &serial
	}

	_, err := p.inventory.ReceiveStock(ctx, request)
	return err
}

// silence the unused-import warning if uuid is not otherwise referenced.
var _ = uuid.Nil

// ---------- goodsreceipt → purchaseorder ----------

type goodsReceiptPurchaseOrderReceiver struct {
	orders *purchaseorderservice.Service
}

var _ goodsreceiptservice.PurchaseOrderReceiver = (*goodsReceiptPurchaseOrderReceiver)(nil)

func newGoodsReceiptPurchaseOrderReceiver(orders *purchaseorderservice.Service) *goodsReceiptPurchaseOrderReceiver {
	return &goodsReceiptPurchaseOrderReceiver{orders: orders}
}

// RecordReceipt books an arrival against the purchase order.
//
// It calls the purchase-order APPLICATION SERVICE, so every rule that aggregate
// enforces applies to stock arriving through a receipt exactly as it does to a
// direct call: a DRAFT or CANCELLED order refuses the receipt, an over-receipt is
// rejected, and PARTIALLY_RECEIVED / COMPLETED are re-derived from the lines.
//
// The call runs inside the goods receipt's transaction — the shared transaction
// manager joins an existing transaction through a SAVEPOINT rather than opening a
// second one — so a refusal here rolls back the receipt, the stock and the ledger
// entries with it. The response is discarded; only the error matters to the
// caller.
func (r *goodsReceiptPurchaseOrderReceiver) RecordReceipt(
	ctx context.Context, receipt goodsreceiptservice.PurchaseOrderReceipt,
) error {
	_, err := r.orders.RecordReceiptForProduct(
		ctx, receipt.OrderID, receipt.ProductID, receipt.Quantity,
	)
	return err
}
