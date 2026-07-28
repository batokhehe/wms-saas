-- Development-only demo tenant. Password hashes are produced by pgcrypto's
-- bcrypt implementation so plaintext credentials never enter the repository.
WITH seed AS (
  SELECT
    '11111111-1111-1111-1111-111111111111'::uuid AS company_id,
    '22222222-2222-2222-2222-222222222222'::uuid AS owner_id,
    '33333333-3333-3333-3333-333333333333'::uuid AS admin_id,
    '44444444-4444-4444-4444-444444444444'::uuid AS warehouse_id
)
INSERT INTO companies (id, code, name, status, created_at, updated_at)
SELECT company_id, 'DEMO', 'Demo Company', 'ACTIVE', NOW(), NOW() FROM seed
ON CONFLICT DO NOTHING;

INSERT INTO users (id, email, password_hash, full_name, status, created_at, updated_at)
VALUES
  ('22222222-2222-2222-2222-222222222222', 'owner@demo.local', crypt('Password123!', gen_salt('bf', 12)), 'Demo Owner', 'ACTIVE', NOW(), NOW()),
  ('33333333-3333-3333-3333-333333333333', 'admin@demo.local', crypt('Password123!', gen_salt('bf', 12)), 'Demo Admin', 'ACTIVE', NOW(), NOW())
ON CONFLICT DO NOTHING;

INSERT INTO memberships (id, company_id, user_id, role, status, joined_at, created_at, updated_at)
VALUES
  ('55555555-5555-5555-5555-555555555555', '11111111-1111-1111-1111-111111111111', '22222222-2222-2222-2222-222222222222', 'OWNER', 'ACTIVE', NOW(), NOW(), NOW()),
  ('66666666-6666-6666-6666-666666666666', '11111111-1111-1111-1111-111111111111', '33333333-3333-3333-3333-333333333333', 'ADMIN', 'ACTIVE', NOW(), NOW(), NOW())
ON CONFLICT DO NOTHING;

INSERT INTO roles (id, company_id, name, description, is_system, created_at, updated_at)
VALUES
  ('77777777-7777-7777-7777-777777777771', '11111111-1111-1111-1111-111111111111', 'OWNER', 'Demo owner role', TRUE, NOW(), NOW()),
  ('77777777-7777-7777-7777-777777777772', '11111111-1111-1111-1111-111111111111', 'ADMIN', 'Demo administrator role', TRUE, NOW(), NOW()),
  ('77777777-7777-7777-7777-777777777773', '11111111-1111-1111-1111-111111111111', 'STAFF', 'Demo staff role', TRUE, NOW(), NOW())
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), '77777777-7777-7777-7777-777777777771', id, NOW(), NOW() FROM permissions
ON CONFLICT DO NOTHING;

INSERT INTO warehouses (id, company_id, code, name, description, type, status, created_by, updated_by, created_at, updated_at)
VALUES ('44444444-4444-4444-4444-444444444444', '11111111-1111-1111-1111-111111111111', 'MAIN', 'Main Warehouse', 'Demo warehouse', 'MAIN', 'ACTIVE', '22222222-2222-2222-2222-222222222222', '22222222-2222-2222-2222-222222222222', NOW(), NOW())
ON CONFLICT DO NOTHING;
