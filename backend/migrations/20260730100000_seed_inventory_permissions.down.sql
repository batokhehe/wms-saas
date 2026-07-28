-- 20260730100000: seed_inventory_permissions (down)
--
-- Grants are removed first: role_permissions holds the foreign key into
-- permissions, so deleting the catalogue rows while grants reference them would
-- fail on the constraint. Only the eight codes this migration added are touched.

DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('inventory.read', 'inventory.create', 'inventory.update', 'inventory.adjust',
                   'inventory.reserve', 'inventory.transfer', 'inventory.lock', 'inventory.cyclecount')
);

DELETE FROM permissions
WHERE code IN ('inventory.read', 'inventory.create', 'inventory.update', 'inventory.adjust',
               'inventory.reserve', 'inventory.transfer', 'inventory.lock', 'inventory.cyclecount');
