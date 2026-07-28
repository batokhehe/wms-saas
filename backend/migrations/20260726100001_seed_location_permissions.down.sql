-- 20260726100001: seed_location_permissions (down)
--
-- Grants are removed first: role_permissions holds the foreign key into
-- permissions, so deleting the catalogue rows while grants reference them would
-- fail on the constraint.
--
-- Only the four codes this migration added are touched. A blanket delete would
-- destroy grants that other migrations created.

DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('location.read', 'location.create', 'location.update', 'location.lock')
);

DELETE FROM permissions
WHERE code IN ('location.read', 'location.create', 'location.update', 'location.lock');
