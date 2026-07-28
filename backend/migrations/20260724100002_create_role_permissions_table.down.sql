-- 20260724100002: create_role_permissions_table (down)
--
-- Dropped FIRST in rollback order: it holds the foreign keys into both roles
-- and permissions.

DROP TABLE IF EXISTS role_permissions;
