-- 20260728100000: create_products_table (down)
--
-- Children first: product_barcodes and product_uoms hold foreign keys into
-- products, so the parent cannot be dropped while they exist. (ON DELETE CASCADE
-- governs row deletion, not DROP TABLE ordering.)
--
-- Dropped before the product permissions, which the next migration seeds.

DROP TABLE IF EXISTS product_uoms;
DROP TABLE IF EXISTS product_barcodes;
DROP TABLE IF EXISTS products;
