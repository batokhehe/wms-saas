-- 20260805100000: create_brands_table (down)
--
-- Dropped after products, which references brands; the reverse rollback order
-- means the product table is already gone by the time this runs.

DROP TABLE IF EXISTS brands;
