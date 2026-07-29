-- 20260809100000: seed_catalog_permissions (up)
--
-- Adds nine capabilities that the brand, category and product ROUTES already
-- enforced but that the permission catalogue never listed.
--
-- The effect of the gap was total: middleware.RequirePermission("brand.read")
-- looks the code up in the caller's granted set, and a code that exists in no
-- catalogue row can be in nobody's set — so every Brand and Category endpoint,
-- and product archive, answered 403 to every caller including OWNER. The modules
-- were unreachable rather than merely unprotected.

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'category.read',   'View categories',   'category', NOW(), NOW()),
    (gen_random_uuid(), 'category.create', 'Create categories', 'category', NOW(), NOW()),
    (gen_random_uuid(), 'category.update', 'Update categories', 'category', NOW(), NOW()),
    (gen_random_uuid(), 'category.delete', 'Delete categories', 'category', NOW(), NOW()),
    (gen_random_uuid(), 'brand.read',      'View brands',       'brand',    NOW(), NOW()),
    (gen_random_uuid(), 'brand.create',    'Create brands',     'brand',    NOW(), NOW()),
    (gen_random_uuid(), 'brand.update',    'Update brands',     'brand',    NOW(), NOW()),
    (gen_random_uuid(), 'brand.delete',    'Delete brands',     'brand',    NOW(), NOW()),
    (gen_random_uuid(), 'product.delete',  'Archive products',  'product',  NOW(), NOW());

-- OWNER: all nine. Ownership must never lack a capability the software has.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r CROSS JOIN permissions p
WHERE r.is_system = TRUE AND r.name = 'OWNER' AND r.deleted_at IS NULL
  AND p.code IN ('category.read','category.create','category.update','category.delete',
                 'brand.read','brand.create','brand.update','brand.delete','product.delete')
  AND NOT EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role_id = r.id AND rp.permission_id = p.id);

-- ADMIN: curate the catalogue, but not delete. Removing a classification that
-- existing products point at is an ownership decision, matching warehouse.delete.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r CROSS JOIN permissions p
WHERE r.is_system = TRUE AND r.name = 'ADMIN' AND r.deleted_at IS NULL
  AND p.code IN ('category.read','category.create','category.update',
                 'brand.read','brand.create','brand.update')
  AND NOT EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role_id = r.id AND rp.permission_id = p.id);

-- STAFF: read only. Staff pick against these classifications; they do not define them.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r CROSS JOIN permissions p
WHERE r.is_system = TRUE AND r.name = 'STAFF' AND r.deleted_at IS NULL
  AND p.code IN ('category.read','brand.read')
  AND NOT EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role_id = r.id AND rp.permission_id = p.id);
