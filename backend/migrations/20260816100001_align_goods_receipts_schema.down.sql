-- 20260816100001: align_goods_receipts_schema (down)

DROP INDEX IF EXISTS idx_goods_receipt_lines_product;
DROP INDEX IF EXISTS idx_goods_receipts_reference;
DROP INDEX IF EXISTS idx_goods_receipts_company_date;
DROP INDEX IF EXISTS idx_goods_receipts_company_status;

ALTER TABLE goods_receipts
    DROP CONSTRAINT IF EXISTS ck_goods_receipts_reference,
    DROP CONSTRAINT IF EXISTS ck_goods_receipts_received_pair,
    DROP CONSTRAINT IF EXISTS ck_goods_receipts_status,
    DROP CONSTRAINT IF EXISTS fk_goods_receipts_received_by,
    DROP CONSTRAINT IF EXISTS fk_goods_receipts_updated_by,
    DROP CONSTRAINT IF EXISTS fk_goods_receipts_created_by;

ALTER TABLE goods_receipts DROP COLUMN IF EXISTS updated_by;

DROP INDEX IF EXISTS idx_goods_receipt_lines_location;

ALTER TABLE goods_receipt_lines
    DROP COLUMN IF EXISTS location_id,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS created_at;

ALTER TABLE goods_receipt_lines
    DROP CONSTRAINT IF EXISTS ck_goods_receipt_lines_lot_xor_serial,
    DROP CONSTRAINT IF EXISTS ck_goods_receipt_lines_quantity_positive;

ALTER TABLE goods_receipt_lines
    ALTER COLUMN quantity TYPE NUMERIC(19, 6) USING quantity::NUMERIC(19, 6);

DROP INDEX IF EXISTS ux_goods_receipts_company_number;
CREATE UNIQUE INDEX ux_goods_receipts_number ON goods_receipts (number);
