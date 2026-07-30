-- 20260815100000: create_purchase_orders_table (up)
--
-- A purchase order is the PLANNING document of the inbound chain:
--
--     PurchaseOrder -> ASN -> GoodsReceipt -> QualityInspection -> Putaway
--
-- It holds no stock. received_qty on a line records how much of the commitment
-- has been satisfied so far; the stock itself lives in inventory_positions.

CREATE TABLE purchase_orders (
    id                    UUID PRIMARY KEY,
    company_id            UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    -- CITEXT so a company cannot register both "PO-001" and "po-001".
    number                CITEXT NOT NULL,

    supplier_id           UUID NOT NULL REFERENCES suppliers (id)  ON DELETE RESTRICT,
    warehouse_id          UUID NOT NULL REFERENCES warehouses (id) ON DELETE RESTRICT,

    order_date            TIMESTAMPTZ NOT NULL,
    expected_arrival_date TIMESTAMPTZ NOT NULL,

    status                VARCHAR(24) NOT NULL DEFAULT 'DRAFT',
    remarks               TEXT NOT NULL DEFAULT '',

    version               BIGINT NOT NULL DEFAULT 1,

    created_by            UUID NOT NULL REFERENCES users (id),
    -- Nullable: an order carries no approver until it is approved, and NULL is
    -- the honest representation of "not yet".
    approved_by           UUID REFERENCES users (id),
    approved_at           TIMESTAMPTZ,
    updated_by            UUID NOT NULL REFERENCES users (id),

    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL,
    deleted_at            TIMESTAMPTZ,

    CONSTRAINT ck_purchase_orders_status
        CHECK (status IN ('DRAFT', 'APPROVED', 'PARTIALLY_RECEIVED', 'COMPLETED', 'CANCELLED')),

    -- An expected arrival before the order date describes a delivery that
    -- precedes the order, which is a data-entry error rather than a plan.
    CONSTRAINT ck_purchase_orders_dates
        CHECK (expected_arrival_date >= order_date),

    -- An approved order has both approval columns or neither. Half-set approval
    -- metadata would make "who approved this?" unanswerable.
    CONSTRAINT ck_purchase_orders_approval_pair
        CHECK ((approved_by IS NULL) = (approved_at IS NULL))
);

-- Partial unique: a number is unique only among a company's LIVE orders, so a
-- soft-deleted draft does not permanently reserve its number.
CREATE UNIQUE INDEX ux_purchase_orders_company_number
    ON purchase_orders (company_id, number)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_purchase_orders_company_status
    ON purchase_orders (company_id, status)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_purchase_orders_company_supplier
    ON purchase_orders (company_id, supplier_id)
    WHERE deleted_at IS NULL;

-- Backs the default listing, which is newest-order-first within a tenant.
CREATE INDEX idx_purchase_orders_company_date
    ON purchase_orders (company_id, order_date DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE purchase_order_lines (
    id                UUID PRIMARY KEY,

    -- CASCADE, not RESTRICT: a line has no life outside its order. The aggregate
    -- root owns the line set and replaces it wholesale.
    purchase_order_id UUID NOT NULL REFERENCES purchase_orders (id) ON DELETE CASCADE,

    product_id        UUID NOT NULL REFERENCES products (id) ON DELETE RESTRICT,
    uom_id            UUID NOT NULL REFERENCES uoms (id)     ON DELETE RESTRICT,

    -- BIGINT, matching inventory_positions. Stock is counted in whole units
    -- everywhere in this system.
    ordered_qty       BIGINT NOT NULL,
    received_qty      BIGINT NOT NULL DEFAULT 0,

    -- Minor units, nullable so "not priced" stays distinct from "free".
    unit_price        BIGINT,

    remarks           TEXT NOT NULL DEFAULT '',

    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,

    CONSTRAINT ck_purchase_order_lines_ordered_positive
        CHECK (ordered_qty > 0),

    -- Over-receipt is refused by the aggregate; this is the database saying the
    -- same thing, so a direct write cannot create a line claiming more arrived
    -- than was ever ordered.
    CONSTRAINT ck_purchase_order_lines_received_range
        CHECK (received_qty >= 0 AND received_qty <= ordered_qty),

    CONSTRAINT ck_purchase_order_lines_price_non_negative
        CHECK (unit_price IS NULL OR unit_price >= 0)
);

CREATE INDEX idx_purchase_order_lines_order
    ON purchase_order_lines (purchase_order_id);

CREATE INDEX idx_purchase_order_lines_product
    ON purchase_order_lines (product_id);

-- One line per product per order. Two lines for one article make receiving
-- ambiguous: a receipt naming a product would have no way to choose between them.
CREATE UNIQUE INDEX ux_purchase_order_lines_order_product
    ON purchase_order_lines (purchase_order_id, product_id);
