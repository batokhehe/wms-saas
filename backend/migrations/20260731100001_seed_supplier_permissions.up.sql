-- 20260731100001: seed_supplier_permissions (up)
--
-- Adds the supplier capabilities to the global catalogue and backfills them onto
-- the system roles of companies that ALREADY exist. The backfill is required
-- because the RBAC provisioner runs once per company at creation and never
-- repairs an existing role (docs/RBAC.md §5); new companies get these grants
-- from the extended entity.DefaultPermissions. A NEW migration rather than an
-- edit to an earlier one, per docs/PermissionMatrix.md §6.

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'supplier.read',     'View suppliers',       'supplier', NOW(), NOW()),
    (gen_random_uuid(), 'supplier.create',   'Create suppliers',     'supplier', NOW(), NOW()),
    (gen_random_uuid(), 'supplier.update',   'Update suppliers',     'supplier', NOW(), NOW()),
    (gen_random_uuid(), 'supplier.activate', 'Activate suppliers',   'supplier', NOW(), NOW());

-- OWNER: all four. Ownership must never lack a capability the software has.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'OWNER'
  AND r.deleted_at IS NULL
  AND p.code IN ('supplier.read', 'supplier.create', 'supplier.update', 'supplier.activate')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- ADMIN: all four. Suppliers are master data an admin curates end to end.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.code IN ('supplier.read', 'supplier.create', 'supplier.update', 'supplier.activate')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- STAFF: read only. Staff consult the supplier catalogue but do not curate it.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'STAFF'
  AND r.deleted_at IS NULL
  AND p.code = 'supplier.read'
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );
