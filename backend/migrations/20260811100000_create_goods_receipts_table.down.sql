-- Child first: goods_receipt_lines has a FK to goods_receipts.
DROP INDEX IF EXISTS ix_goods_receipts_company;
DROP INDEX IF EXISTS ux_goods_receipts_number;
DROP TABLE IF EXISTS goods_receipt_lines;
DROP TABLE IF EXISTS goods_receipts;
