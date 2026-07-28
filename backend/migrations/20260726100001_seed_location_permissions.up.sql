-- 20260726100001: seed_location_permissions (up)
--
-- Adds the location capabilities to the global catalogue and backfills them
-- onto the system roles of companies that ALREADY exist.
--
-- The backfill is required for the same reason as the warehouse sprint's: the
-- RBAC provisioner deliberately never repairs an existing role (docs/RBAC.md
-- §5), so without it every company created before this migration would have an
-- OWNER who cannot manage locations at all.
--
-- A NEW migration rather than an edit to an earlier one, per
-- docs/PermissionMatrix.md §6 — the seed migrations have already run
-- everywhere, and editing one would change its checksum and desynchronise every
-- environment.

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'location.read',   'View storage locations',   'location', NOW(), NOW()),
    (gen_random_uuid(), 'location.create', 'Create storage locations', 'location', NOW(), NOW()),
    (gen_random_uuid(), 'location.update', 'Update storage locations', 'location', NOW(), NOW()),
    (gen_random_uuid(), 'location.lock',   'Lock storage locations',   'location', NOW(), NOW());

-- OWNER: all four. Ownership must never lack a capability the software has.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'OWNER'
  AND r.deleted_at IS NULL
  AND p.code IN ('location.read', 'location.create', 'location.update', 'location.lock')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- ADMIN: all four, including lock.
--
-- This differs from warehouse.delete, which ADMIN does not get. Locking a bin
-- because a rack is damaged is a day-to-day operational decision made by the
-- person running the floor; archiving an entire site is not. Withholding it
-- would mean a damaged rack stays pickable until an owner is available.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.code IN ('location.read', 'location.create', 'location.update', 'location.lock')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- STAFF: read only.
--
-- Staff work INSIDE locations — they will put stock away and pick from them —
-- but defining the rack layout is not their decision. The permissions for
-- operating on stock belong to the Inventory sprint, not this one.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'STAFF'
  AND r.deleted_at IS NULL
  AND p.code = 'location.read'
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );
