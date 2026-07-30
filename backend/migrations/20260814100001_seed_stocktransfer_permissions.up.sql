-- 20260814100001: seed_stocktransfer_permissions (up)
--
-- Six codes, one per capability the stock-transfer routes enforce.
--
-- The three lifecycle codes are separate because they authorise materially
-- different things. Confirming approves a movement on paper; COMPLETING MOVES
-- REAL STOCK and is the only one of the three that changes a balance; cancelling
-- voids an approved document. Folding them into stocktransfer.update would mean
-- anyone who can fix a typo in the remarks can also execute the movement.
--
-- Backfilled onto existing companies' system roles for the usual reason: the RBAC
-- provisioner runs once per company at creation and never repairs an existing
-- role (docs/RBAC.md §5).

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'stocktransfer.read',     'View stock transfers',     'stocktransfer', NOW(), NOW()),
    (gen_random_uuid(), 'stocktransfer.create',   'Draft a stock transfer',   'stocktransfer', NOW(), NOW()),
    (gen_random_uuid(), 'stocktransfer.update',   'Edit a draft transfer',    'stocktransfer', NOW(), NOW()),
    (gen_random_uuid(), 'stocktransfer.confirm',  'Confirm a stock transfer', 'stocktransfer', NOW(), NOW()),
    (gen_random_uuid(), 'stocktransfer.complete', 'Execute a stock transfer', 'stocktransfer', NOW(), NOW()),
    (gen_random_uuid(), 'stocktransfer.cancel',   'Cancel a stock transfer',  'stocktransfer', NOW(), NOW());

-- OWNER: everything.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE AND r.name = 'OWNER' AND r.deleted_at IS NULL
  AND p.code IN ('stocktransfer.read', 'stocktransfer.create', 'stocktransfer.update',
                 'stocktransfer.confirm', 'stocktransfer.complete', 'stocktransfer.cancel')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- ADMIN runs transfers day to day: draft, edit, approve and call off. Only
-- COMPLETE is withheld, on the same principle as inventory.adjust — it is the
-- step that actually changes a balance, so a company that wants a second pair of
-- eyes on stock movements can grant it separately.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE AND r.name = 'ADMIN' AND r.deleted_at IS NULL
  AND p.code IN ('stocktransfer.read', 'stocktransfer.create', 'stocktransfer.update',
                 'stocktransfer.confirm', 'stocktransfer.cancel')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );

-- STAFF are the people who physically move the pallet, so they read transfers and
-- mark them done. They do not decide WHAT moves: drafting, editing, approving and
-- cancelling are all withheld.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE AND r.name = 'STAFF' AND r.deleted_at IS NULL
  AND p.code IN ('stocktransfer.read', 'stocktransfer.complete')
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );
