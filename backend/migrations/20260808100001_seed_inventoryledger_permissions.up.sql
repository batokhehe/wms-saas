-- 20260808100001: seed_inventoryledger_permissions (up)
--
-- The ledger is read-only over HTTP, so it has exactly ONE capability. There is
-- no create/update/delete code because there is no such operation to grant.
--
-- Backfilled onto existing companies' system roles for the usual reason: the RBAC
-- provisioner runs once per company at creation and never repairs an existing
-- role (docs/RBAC.md §5).

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'inventoryledger.read', 'View inventory ledger', 'inventoryledger', NOW(), NOW());

-- OWNER, ADMIN and STAFF all read it. An audit trail nobody on the floor can
-- consult cannot answer "where did my stock go?", which is the only reason it
-- exists; and reading it grants no ability to change anything.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name IN ('OWNER', 'ADMIN', 'STAFF')
  AND r.deleted_at IS NULL
  AND p.code = 'inventoryledger.read'
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );
