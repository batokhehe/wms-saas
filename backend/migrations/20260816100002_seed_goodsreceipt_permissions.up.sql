-- 20260816100002: seed_goodsreceipt_permissions (up)
--
-- Six codes. goodsreceipt.receive is separate from the rest because it is the one
-- that POSTS STOCK: confirming says the delivery was checked, receiving books it
-- into inventory and appends to the ledger. Folding it into goodsreceipt.update
-- would mean anyone who can fix a typo in the remarks can create inventory.
--
-- Backfilled onto existing companies' system roles: the RBAC provisioner runs once
-- per company at creation and never repairs an existing role (docs/RBAC.md §5).

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'goodsreceipt.read',    'View goods receipts',      'goodsreceipt', NOW(), NOW()),
    (gen_random_uuid(), 'goodsreceipt.create',  'Draft a goods receipt',    'goodsreceipt', NOW(), NOW()),
    (gen_random_uuid(), 'goodsreceipt.update',  'Edit a draft receipt',     'goodsreceipt', NOW(), NOW()),
    (gen_random_uuid(), 'goodsreceipt.confirm', 'Confirm a goods receipt',  'goodsreceipt', NOW(), NOW()),
    (gen_random_uuid(), 'goodsreceipt.receive', 'Post received stock',      'goodsreceipt', NOW(), NOW()),
    (gen_random_uuid(), 'goodsreceipt.cancel',  'Cancel a goods receipt',   'goodsreceipt', NOW(), NOW());

-- OWNER: everything.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r CROSS JOIN permissions p
WHERE r.is_system = TRUE AND r.name = 'OWNER' AND r.deleted_at IS NULL
  AND p.module = 'goodsreceipt'
  AND NOT EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role_id = r.id AND rp.permission_id = p.id);

-- ADMIN runs the paperwork but does not post stock.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r CROSS JOIN permissions p
WHERE r.is_system = TRUE AND r.name = 'ADMIN' AND r.deleted_at IS NULL
  AND p.code IN ('goodsreceipt.read', 'goodsreceipt.create', 'goodsreceipt.update',
                 'goodsreceipt.confirm', 'goodsreceipt.cancel')
  AND NOT EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role_id = r.id AND rp.permission_id = p.id);

-- STAFF are the people on the dock: they raise the receipt, check it off and book
-- the stock in. Cancelling a delivery is not their call.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r CROSS JOIN permissions p
WHERE r.is_system = TRUE AND r.name = 'STAFF' AND r.deleted_at IS NULL
  AND p.code IN ('goodsreceipt.read', 'goodsreceipt.create', 'goodsreceipt.update',
                 'goodsreceipt.confirm', 'goodsreceipt.receive')
  AND NOT EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role_id = r.id AND rp.permission_id = p.id);
