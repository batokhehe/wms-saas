-- 20260816100000: seed_uom_permissions (down)
-- Grants first: role_permissions references permissions.
DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code = 'uom.read');

DELETE FROM permissions WHERE code = 'uom.read';
