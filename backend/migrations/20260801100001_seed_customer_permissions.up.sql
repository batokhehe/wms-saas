-- 20260801100001: seed_customer_permissions (up)
--
-- Adds the customer capabilities to the global catalogue and backfills them onto
-- the system roles of companies that ALREADY exist. The backfill is required
-- because the RBAC provisioner runs once per company at creation and never
-- repairs an existing role (docs/RBAC.md §5); new companies get these grants from
-- the extended entity.DefaultPermissions. A NEW migration rather than an edit to
-- an earlier one, per docs/PermissionMatrix.md §6.

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'customer.read',     'View customers',     'customer', NOW(), NOW()),
    (gen_random_uuid(), 'customer.create',   'Create customers',   'customer', NOW(), NOW()),
    (gen_random_uuid(), 'customer.update',   'Update customers',   'customer', NOW(), NOW()),
    (gen_random_uuid(), 'customer.activate', 'Activate customers', 'customer', NOW(), NOW());

-- OWNER: all four.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'OWNER'
  AND r.deleted_at IS NULL
  AND p.code IN ('customer.read', 'customer.create', 'customer.update', 'customer.activate')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- ADMIN: all four. Customers are master data an admin curates end to end.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.code IN ('customer.read', 'customer.create', 'customer.update', 'customer.activate')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- STAFF: read only. Staff consult the customer catalogue but do not curate it.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'STAFF'
  AND r.deleted_at IS NULL
  AND p.code = 'customer.read'
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );
