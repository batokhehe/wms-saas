-- 20260816100001: align_goods_receipts_schema (up)
--
-- The goods-receipt tables were created ahead of the module and carry three
-- defects that block it from becoming a working vertical slice.
--
-- 1. ux_goods_receipts_number was GLOBALLY unique. A number is an operator-facing
--    document reference, and one company taking "GR-001" silently blocked every
--    other tenant from ever using it — a cross-tenant conflict, and a disclosure
--    channel: a duplicate-key error told you another company existed and what it
--    had numbered. Replaced with the partial per-company unique index the rest of
--    the schema uses.
--
-- 2. quantity was NUMERIC(19,6). Stock is counted in whole units everywhere else
--    (inventory_positions.available, purchase_order_lines.ordered_qty are BIGINT).
--    A fractional receipt could not be posted to a position that cannot hold one,
--    so the receipt and the stock it creates could never reconcile.
--
-- 3. created_by / received_by had no foreign key, and there was no updated_by at
--    all, so "who last touched this document" was unanswerable.

-- ---------- 1. tenant-scoped document number ----------
DROP INDEX IF EXISTS ux_goods_receipts_number;

CREATE UNIQUE INDEX ux_goods_receipts_company_number
    ON goods_receipts (company_id, number)
    WHERE deleted_at IS NULL;

-- ---------- 2. whole-unit quantities ----------
ALTER TABLE goods_receipt_lines
    ALTER COLUMN quantity TYPE BIGINT USING ROUND(quantity)::BIGINT;

ALTER TABLE goods_receipt_lines
    ADD CONSTRAINT ck_goods_receipt_lines_quantity_positive CHECK (quantity > 0);

-- A line names either a lot or a serial set, never both: they are different ways
-- of individuating the same units.
ALTER TABLE goods_receipt_lines
    ADD CONSTRAINT ck_goods_receipt_lines_lot_xor_serial
    CHECK (lot_number IS NULL OR serial_numbers IS NULL);

ALTER TABLE goods_receipt_lines
    ALTER COLUMN remarks SET DEFAULT '',
    ALTER COLUMN remarks SET NOT NULL;

ALTER TABLE goods_receipt_lines
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- A receipt line had no destination. Stock cannot be posted to a warehouse alone:
-- an inventory position is keyed by warehouse AND location, so without this the
-- receipt could never reach inventory. Goods land in a receiving/staging bin and
-- are moved to their final home by Putaway, which is why the column lives on the
-- line rather than the header — a single delivery can be broken across bins.
ALTER TABLE goods_receipt_lines
    ADD COLUMN location_id UUID NOT NULL REFERENCES storage_locations (id) ON DELETE RESTRICT;

CREATE INDEX idx_goods_receipt_lines_location
    ON goods_receipt_lines (location_id);

-- ---------- 3. audit integrity ----------
ALTER TABLE goods_receipts
    ADD COLUMN updated_by UUID;

-- Existing rows (there are none in practice) inherit their creator so the column
-- can be made NOT NULL without inventing an actor.
UPDATE goods_receipts SET updated_by = created_by WHERE updated_by IS NULL;

ALTER TABLE goods_receipts
    ALTER COLUMN updated_by SET NOT NULL;

ALTER TABLE goods_receipts
    ADD CONSTRAINT fk_goods_receipts_created_by  FOREIGN KEY (created_by)  REFERENCES users (id),
    ADD CONSTRAINT fk_goods_receipts_updated_by  FOREIGN KEY (updated_by)  REFERENCES users (id),
    ADD CONSTRAINT fk_goods_receipts_received_by FOREIGN KEY (received_by) REFERENCES users (id);

ALTER TABLE goods_receipts
    ALTER COLUMN remarks SET DEFAULT '',
    ALTER COLUMN remarks SET NOT NULL;

ALTER TABLE goods_receipts
    ADD CONSTRAINT ck_goods_receipts_status
    CHECK (status IN ('DRAFT', 'CONFIRMED', 'RECEIVED', 'CANCELLED'));

-- A received document has both receipt columns or neither; half-set metadata
-- makes "who booked this stock?" unanswerable.
ALTER TABLE goods_receipts
    ADD CONSTRAINT ck_goods_receipts_received_pair
    CHECK ((status = 'RECEIVED') = (received_by IS NOT NULL));

-- A reference id without a type is unresolvable: nothing says which document it
-- points at.
ALTER TABLE goods_receipts
    ADD CONSTRAINT ck_goods_receipts_reference
    CHECK (reference_id IS NULL OR reference_type <> 'NONE');

CREATE INDEX idx_goods_receipts_company_status
    ON goods_receipts (company_id, status) WHERE deleted_at IS NULL;

CREATE INDEX idx_goods_receipts_company_date
    ON goods_receipts (company_id, receipt_date DESC) WHERE deleted_at IS NULL;

-- Backs "which receipts booked against this purchase order?".
CREATE INDEX idx_goods_receipts_reference
    ON goods_receipts (company_id, reference_type, reference_id)
    WHERE deleted_at IS NULL AND reference_id IS NOT NULL;

CREATE INDEX idx_goods_receipt_lines_product
    ON goods_receipt_lines (product_id);
