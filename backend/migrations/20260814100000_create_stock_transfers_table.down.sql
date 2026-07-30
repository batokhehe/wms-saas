-- 20260814100000: create_stock_transfers_table (down)
--
-- Child first: stock_transfer_lines has a FK to stock_transfers.
DROP INDEX IF EXISTS idx_stock_transfer_lines_product;
DROP INDEX IF EXISTS idx_stock_transfer_lines_transfer;
DROP TABLE IF EXISTS stock_transfer_lines;

DROP INDEX IF EXISTS idx_stock_transfers_company_date;
DROP INDEX IF EXISTS idx_stock_transfers_company_status;
DROP INDEX IF EXISTS ux_stock_transfers_company_number;
DROP TABLE IF EXISTS stock_transfers;
