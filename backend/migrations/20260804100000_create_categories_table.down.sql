-- 20260804100000: create_categories_table (down)
--
-- Dropped after products, which references categories: golang-migrate rolls back
-- in reverse order, so the product table is already gone by the time this runs.

DROP TABLE IF EXISTS categories;
