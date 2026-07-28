-- 20260723100000: create_companies_table (down)
--
-- Dropped after memberships in rollback order, because memberships holds the
-- foreign key into this table.

DROP TABLE IF EXISTS companies;
