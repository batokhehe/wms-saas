-- 20260724100000: create_permissions_table (down)
--
-- Dropped after role_permissions in rollback order, because that table holds
-- the foreign key into this one. The seeded catalogue goes with the table.

DROP TABLE IF EXISTS permissions;
