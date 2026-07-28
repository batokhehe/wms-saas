-- 20260730100000: seed_inventory_permissions (up)
--
-- Adds the inventory capabilities to the global catalogue and backfills them
-- onto the system roles of companies that ALREADY exist.
--
-- The backfill is required for the same reason as every prior domain sprint's:
-- the RBAC provisioner runs once per company at creation and never repairs an
-- existing role (docs/RBAC.md §5). Without it, every company created before this
-- migration would have an OWNER who cannot manage stock. New companies get these
-- grants automatically from the extended entity.DefaultPermissions.
--
-- A NEW migration rather than an edit to an earlier one, per
-- docs/PermissionMatrix.md §6.

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'inventory.read',       'View inventory',           'inventory', NOW(), NOW()),
    (gen_random_uuid(), 'inventory.create',     'Create inventory',         'inventory', NOW(), NOW()),
    (gen_random_uuid(), 'inventory.update',     'Move inventory quantity',  'inventory', NOW(), NOW()),
    (gen_random_uuid(), 'inventory.adjust',     'Adjust inventory',         'inventory', NOW(), NOW()),
    (gen_random_uuid(), 'inventory.reserve',    'Reserve inventory',        'inventory', NOW(), NOW()),
    (gen_random_uuid(), 'inventory.transfer',   'Transfer inventory',       'inventory', NOW(), NOW()),
    (gen_random_uuid(), 'inventory.lock',       'Lock inventory',           'inventory', NOW(), NOW()),
    (gen_random_uuid(), 'inventory.cyclecount', 'Cycle-count inventory',    'inventory', NOW(), NOW());

-- OWNER: all eight. Ownership must never lack a capability the software has.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'OWNER'
  AND r.deleted_at IS NULL
  AND p.code IN ('inventory.read', 'inventory.create', 'inventory.update', 'inventory.adjust',
                 'inventory.reserve', 'inventory.transfer', 'inventory.lock', 'inventory.cyclecount')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- ADMIN: all except adjust. A manual adjustment overrides the count with no
-- physical count behind it — an ownership decision, like warehouse.delete.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'ADMIN'
  AND r.deleted_at IS NULL
  AND p.code IN ('inventory.read', 'inventory.create', 'inventory.update',
                 'inventory.reserve', 'inventory.transfer', 'inventory.lock', 'inventory.cyclecount')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- STAFF: the operational stock actions. Staff move, reserve, transfer and count
-- stock, but do not open positions, make manual adjustments or place governance
-- locks. This is the sprint the earlier modules deferred stock permissions to.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name = 'STAFF'
  AND r.deleted_at IS NULL
  AND p.code IN ('inventory.read', 'inventory.update', 'inventory.reserve',
                 'inventory.transfer', 'inventory.cyclecount')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );
