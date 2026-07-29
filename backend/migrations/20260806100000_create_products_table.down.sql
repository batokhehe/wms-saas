-- 20260806100000: create_products_table (down)
--
-- Children first: product_barcodes and product_alternate_uoms hold foreign keys
-- into products, so the parent cannot be dropped while they exist. (ON DELETE
-- governs row deletion, not DROP TABLE ordering.)

DROP TABLE IF EXISTS product_alternate_uoms;
DROP TABLE IF EXISTS product_barcodes;
DROP TABLE IF EXISTS products;
