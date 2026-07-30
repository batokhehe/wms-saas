-- 20260815100000: create_purchase_orders_table (down)
--
-- Child first: purchase_order_lines has a FK to purchase_orders.
DROP INDEX IF EXISTS ux_purchase_order_lines_order_product;
DROP INDEX IF EXISTS idx_purchase_order_lines_product;
DROP INDEX IF EXISTS idx_purchase_order_lines_order;
DROP TABLE IF EXISTS purchase_order_lines;

DROP INDEX IF EXISTS idx_purchase_orders_company_date;
DROP INDEX IF EXISTS idx_purchase_orders_company_supplier;
DROP INDEX IF EXISTS idx_purchase_orders_company_status;
DROP INDEX IF EXISTS ux_purchase_orders_company_number;
DROP TABLE IF EXISTS purchase_orders;
