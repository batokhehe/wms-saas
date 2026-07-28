-- 20260725100001: seed_warehouse_permissions (up)
--
-- Adds the warehouse capabilities to the global permission catalogue and grants
-- them to the system roles of companies that ALREADY exist.
--
-- Two things happen here, and the second is the one that matters.
--
-- # 1. The catalogue rows
--
-- A NEW migration rather than an edit to 20260724100000, per
-- docs/PermissionMatrix.md §6: the seed migration has already run everywhere,
-- so editing it would change a checksum and desynchronise every environment.
--
-- # 2. The backfill
--
-- The RBAC provisioner deliberately never repairs an existing role — silently
-- re-adding a permission an administrator revoked would be a security
-- regression disguised as a fix (docs/RBAC.md §5).
--
-- That correct behaviour has a consequence here: without a backfill, every
-- company created before this migration would have an OWNER who cannot manage
-- warehouses at all. Their roles exist, so provisioning skips them, and the new
-- permissions would reach only companies created afterwards.
--
-- So the grants are written explicitly, once, matching
-- entity.DefaultPermissions exactly. This is a one-time correction tied to the
-- introduction of a capability, not a general repair — it grants only the five
-- codes this migration adds, and touches nothing an administrator has chosen.

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'warehouse.read',     'View warehouses',     'warehouse', NOW(), NOW()),
    (gen_random_uuid(), 'warehouse.create',   'Create warehouses',   'warehouse', NOW(), NOW()),
    (gen_random_uuid(), 'warehouse.update',   'Update warehouses',   'warehouse', NOW(), NOW()),
    (gen_random_uuid(), 'warehouse.delete',   'Archive warehouses',  'warehouse', NOW(), NOW()),
    (gen_random_uuid(), 'warehouse.activate', 'Activate warehouses', 'warehouse', NOW(), NOW());

-- OWNER: all five. Ownership is the recovery path for every authorisation
-- mistake, so it must never lack a capability the software has.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'OWNER'
  AND r.deleted_at IS NULL
  AND p.code IN ('warehouse.read', 'warehouse.create', 'warehouse.update',
                 'warehouse.delete', 'warehouse.activate')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- ADMIN: operational, but not destructive. warehouse.delete is withheld for the
-- same reason as company.delete — archiving a site is an ownership decision,
-- not a day-to-day one.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.code IN ('warehouse.read', 'warehouse.create', 'warehouse.update',
                 'warehouse.activate')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- STAFF: read only. Staff operate WITHIN a warehouse; they do not define one.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'STAFF'
  AND r.deleted_at IS NULL
  AND p.code = 'warehouse.read'
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );
