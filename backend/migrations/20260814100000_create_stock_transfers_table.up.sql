-- 20260814100000: create_stock_transfers_table (up)
--
-- A stock transfer is a DOCUMENT. It records an intent to move stock and whether
-- that intent was executed; it never holds a balance. Total quantity is therefore
-- invariant across a transfer, and this schema helps guarantee that by having no
-- column anywhere that could store one.

CREATE TABLE stock_transfers (
    id                UUID PRIMARY KEY,
    company_id        UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    -- CITEXT so a company cannot register both "ST-001" and "st-001".
    number            CITEXT NOT NULL,

    -- from_warehouse_id = to_warehouse_id is the SAME-WAREHOUSE case and is
    -- legitimate: moving a pallet from a receiving bay to a rack inside one
    -- building is the commonest transfer there is. No CHECK forbids it.
    from_warehouse_id UUID NOT NULL REFERENCES warehouses (id) ON DELETE RESTRICT,
    to_warehouse_id   UUID NOT NULL REFERENCES warehouses (id) ON DELETE RESTRICT,

    status            VARCHAR(16) NOT NULL DEFAULT 'DRAFT',
    transfer_date     TIMESTAMPTZ NOT NULL,
    remarks           TEXT NOT NULL DEFAULT '',

    version           BIGINT NOT NULL DEFAULT 1,

    created_by        UUID NOT NULL REFERENCES users (id),
    updated_by        UUID NOT NULL REFERENCES users (id),
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    deleted_at        TIMESTAMPTZ,

    CONSTRAINT ck_stock_transfers_status
        CHECK (status IN ('DRAFT', 'CONFIRMED', 'COMPLETED', 'CANCELLED'))
);

-- Partial unique: a number is unique only among a company's LIVE transfers, so a
-- soft-deleted document does not permanently reserve its number.
CREATE UNIQUE INDEX ux_stock_transfers_company_number
    ON stock_transfers (company_id, number)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_stock_transfers_company_status
    ON stock_transfers (company_id, status)
    WHERE deleted_at IS NULL;

-- Backs the default listing, which is newest-document-first within a tenant.
CREATE INDEX idx_stock_transfers_company_date
    ON stock_transfers (company_id, transfer_date DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE stock_transfer_lines (
    id                UUID PRIMARY KEY,

    -- CASCADE, not RESTRICT: a line has no life outside its transfer. The
    -- aggregate root owns the line set and replaces it wholesale.
    stock_transfer_id UUID NOT NULL REFERENCES stock_transfers (id) ON DELETE CASCADE,

    product_id        UUID NOT NULL REFERENCES products (id)          ON DELETE RESTRICT,
    from_location_id  UUID NOT NULL REFERENCES storage_locations (id) ON DELETE RESTRICT,
    to_location_id    UUID NOT NULL REFERENCES storage_locations (id) ON DELETE RESTRICT,

    -- BIGINT, matching inventory_positions. Stock is counted in whole units
    -- everywhere in this system; a fractional bucket would not reconcile with a
    -- position that cannot hold one.
    quantity          BIGINT NOT NULL,

    batch_number      VARCHAR(64),
    lot_number        VARCHAR(64),

    -- One serial per unit when the product is serial-tracked; NULL otherwise.
    -- The count rule is an aggregate invariant, not a CHECK, because it relates
    -- the array length to another column and would need a trigger here.
    serial_numbers    TEXT[],

    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,

    CONSTRAINT ck_stock_transfer_lines_quantity_positive
        CHECK (quantity > 0),

    -- A line that moves stock to where it already is changes nothing but would
    -- still be executed and would still append a ledger entry, recording a
    -- movement that did not happen.
    CONSTRAINT ck_stock_transfer_lines_distinct_locations
        CHECK (from_location_id <> to_location_id)
);

CREATE INDEX idx_stock_transfer_lines_transfer
    ON stock_transfer_lines (stock_transfer_id);

CREATE INDEX idx_stock_transfer_lines_product
    ON stock_transfer_lines (product_id);
