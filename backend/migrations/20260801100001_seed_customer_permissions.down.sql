-- 20260801100001: seed_customer_permissions (down)
--
-- Grants first (role_permissions holds the FK into permissions), then the
-- catalogue rows. Only the four codes this migration added are touched.

DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('customer.read', 'customer.create', 'customer.update', 'customer.activate')
);

DELETE FROM permissions
WHERE code IN ('customer.read', 'customer.create', 'customer.update', 'customer.activate');
