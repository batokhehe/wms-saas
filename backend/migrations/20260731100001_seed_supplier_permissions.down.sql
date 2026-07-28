-- 20260731100001: seed_supplier_permissions (down)
--
-- Grants first (role_permissions holds the FK into permissions), then the
-- catalogue rows. Only the four codes this migration added are touched.

DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('supplier.read', 'supplier.create', 'supplier.update', 'supplier.activate')
);

DELETE FROM permissions
WHERE code IN ('supplier.read', 'supplier.create', 'supplier.update', 'supplier.activate');
