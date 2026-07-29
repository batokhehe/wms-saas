-- 20260808100001: seed_inventoryledger_permissions (down)
--
-- Grants first: role_permissions holds the foreign key into permissions.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'inventoryledger.read');

DELETE FROM permissions WHERE code = 'inventoryledger.read';
