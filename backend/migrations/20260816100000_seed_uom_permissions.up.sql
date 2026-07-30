-- 20260816100000: seed_uom_permissions (up)
--
-- uom.read was enforced by the /lookups/uoms route but was absent from the
-- catalogue, so it could never be granted and that endpoint answered 403 to every
-- caller INCLUDING OWNER. Units of measure are referenced by every product and
-- every order line, so a client that cannot resolve them cannot render a product
-- or a purchase-order line at all.
--
-- Backfilled onto existing companies' system roles for the usual reason: the RBAC
-- provisioner runs once per company at creation and never repairs an existing
-- role (docs/RBAC.md §5).

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'uom.read', 'View units of measure', 'uom', NOW(), NOW());

-- Every system role reads it. UOM is global reference data, and reading it grants
-- no ability to change anything.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r
CROSS JOIN permissions p
WHERE r.is_system = TRUE
  AND r.name IN ('OWNER', 'ADMIN', 'STAFF')
  AND r.deleted_at IS NULL
  AND p.code = 'uom.read'
  AND NOT EXISTS (
      SELECT 1 FROM role_permissions rp
      WHERE rp.role_id = r.id AND rp.permission_id = p.id
  );
