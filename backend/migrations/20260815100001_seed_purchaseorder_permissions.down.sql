-- 20260815100001: seed_purchaseorder_permissions (down)
--
-- Grants first: role_permissions references permissions.
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('purchaseorder.read', 'purchaseorder.create', 'purchaseorder.update',
                   'purchaseorder.approve', 'purchaseorder.cancel')
);

DELETE FROM permissions
WHERE code IN ('purchaseorder.read', 'purchaseorder.create', 'purchaseorder.update',
               'purchaseorder.approve', 'purchaseorder.cancel');
