-- 20260723100001: create_memberships_table (down)
--
-- Dropped before companies, because this table holds the foreign key. Rolling
-- back in the wrong order would fail on the constraint.

DROP TABLE IF EXISTS memberships;
