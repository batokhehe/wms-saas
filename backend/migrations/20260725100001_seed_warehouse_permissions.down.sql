-- 20260725100001: seed_warehouse_permissions (down)
--
-- The grants are removed first: role_permissions holds the foreign key into
-- permissions, so deleting the catalogue rows while grants reference them would
-- fail on the constraint.
--
-- Only the five codes this migration added are touched. A blanket delete would
-- destroy grants that other migrations created.

DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('warehouse.read', 'warehouse.create', 'warehouse.update',
                   'warehouse.delete', 'warehouse.activate')
);

DELETE FROM permissions
WHERE code IN ('warehouse.read', 'warehouse.create', 'warehouse.update',
               'warehouse.delete', 'warehouse.activate');
