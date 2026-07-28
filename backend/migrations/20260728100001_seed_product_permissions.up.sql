-- 20260728100001: seed_product_permissions (up)
--
-- Adds the product capabilities to the global catalogue and backfills them onto
-- the system roles of companies that ALREADY exist.
--
-- The backfill is required for the same reason as every prior domain sprint's:
-- the RBAC provisioner runs once per company at creation and deliberately never
-- repairs an existing role (docs/RBAC.md §5). Without this, every company
-- created before this migration would have an OWNER who cannot manage products
-- at all. New companies get these grants automatically from the extended
-- entity.DefaultPermissions — see docs/Product.md §9.
--
-- A NEW migration rather than an edit to an earlier one, per
-- docs/PermissionMatrix.md §6: the seed migrations have already run everywhere,
-- and editing one would change its checksum and desynchronise every environment.

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'product.read',        'View products',       'product', NOW(), NOW()),
    (gen_random_uuid(), 'product.create',      'Create products',     'product', NOW(), NOW()),
    (gen_random_uuid(), 'product.update',      'Update products',     'product', NOW(), NOW()),
    (gen_random_uuid(), 'product.activate',    'Activate products',   'product', NOW(), NOW()),
    (gen_random_uuid(), 'product.discontinue', 'Discontinue products','product', NOW(), NOW());

-- OWNER: all five. Ownership must never lack a capability the software has.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'OWNER'
  AND r.deleted_at IS NULL
  AND p.code IN ('product.read', 'product.create', 'product.update', 'product.activate', 'product.discontinue')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- ADMIN: read, create, update, activate — NOT discontinue.
--
-- This mirrors warehouse.delete, which ADMIN also does not get. Activating a
-- product for operations is a day-to-day catalogue decision; retiring one
-- permanently (DISCONTINUED is terminal) removes it from every future operation
-- and is an ownership decision, not an operational one.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.code IN ('product.read', 'product.create', 'product.update', 'product.activate')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- STAFF: read only.
--
-- Staff pick and receive the products defined here, but defining the catalogue
-- is not their decision. Permissions for acting on STOCK belong to the
-- Inventory sprint, not this one.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'STAFF'
  AND r.deleted_at IS NULL
  AND p.code = 'product.read'
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );
