-- 20260815100001: seed_purchaseorder_permissions (up)
--
-- Five codes. approve and cancel are deliberately NOT folded into
-- purchaseorder.update: editing a draft is clerical, while approving commits the
-- company to buy something and unlocks the whole inbound chain. Folding them in
-- would mean anyone who can fix a typo in the remarks can commit the company's
-- money.
--
-- Backfilled onto existing companies' system roles for the usual reason: the RBAC
-- provisioner runs once per company at creation and never repairs an existing
-- role (docs/RBAC.md §5).

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'purchaseorder.read',    'View purchase orders',    'purchaseorder', NOW(), NOW()),
    (gen_random_uuid(), 'purchaseorder.create',  'Draft a purchase order',  'purchaseorder', NOW(), NOW()),
    (gen_random_uuid(), 'purchaseorder.update',  'Edit a draft order',      'purchaseorder', NOW(), NOW()),
    (gen_random_uuid(), 'purchaseorder.approve', 'Approve a purchase order','purchaseorder', NOW(), NOW()),
    (gen_random_uuid(), 'purchaseorder.cancel',  'Cancel a purchase order', 'purchaseorder', NOW(), NOW());

-- OWNER: everything.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE AND r.name = 'OWNER' AND r.deleted_at IS NULL
  AND p.code IN ('purchaseorder.read', 'purchaseorder.create', 'purchaseorder.update',
                 'purchaseorder.approve', 'purchaseorder.cancel')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- ADMIN drafts and cancels orders but does NOT approve them. Approval is the
-- point at which the company is committed to spend, which is an ownership
-- decision on the same principle as company.delete and inventory.adjust.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE AND r.name = 'ADMIN' AND r.deleted_at IS NULL
  AND p.code IN ('purchaseorder.read', 'purchaseorder.create', 'purchaseorder.update',
                 'purchaseorder.cancel')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- STAFF read orders so the floor can see what is expected to arrive. They do not
-- create, edit, approve or cancel them.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE AND r.name = 'STAFF' AND r.deleted_at IS NULL
  AND p.code = 'purchaseorder.read'
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );
