-- 20260724100000: create_permissions_table (up)
--
-- Permissions are a GLOBAL catalogue, not tenant-owned. There is deliberately
-- no company_id here.
--
-- A permission names a capability the software has ("delete a company",
-- "invite a member"). That set is determined by what the code can do, not by
-- what any customer bought, so it is identical for every tenant. Per-tenant
-- permission rows would mean N copies of the same immutable list, drifting the
-- moment one tenant's seed ran and another's did not — and a permission check
-- would have to ask "does THIS tenant know about this capability?" before it
-- could ask "is this allowed?".
--
-- What IS tenant-owned is the mapping from roles to permissions, which lives in
-- role_permissions.

CREATE TABLE permissions (
    id          UUID PRIMARY KEY,

    -- Code is the stable, machine-readable identifier used in code and in the
    -- middleware: "company.update", "membership.invite". It is the API of the
    -- permission system and must never be renamed once released — a renamed
    -- code silently revokes access everywhere it was granted.
    code        VARCHAR(64)  NOT NULL,

    name        VARCHAR(128) NOT NULL,

    -- Module groups permissions for display ("company", "membership", "role").
    -- It is presentation metadata, never used for authorisation decisions.
    module      VARCHAR(64)  NOT NULL,

    created_at  TIMESTAMPTZ  NOT NULL,
    updated_at  TIMESTAMPTZ  NOT NULL,
    deleted_at  TIMESTAMPTZ
);

-- Partial unique index, matching the project's soft-delete convention.
CREATE UNIQUE INDEX ux_permissions_code ON permissions (code) WHERE deleted_at IS NULL;

-- The permission list endpoint groups by module.
CREATE INDEX idx_permissions_module ON permissions (module) WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Seed the catalogue.
--
-- This is reference data the application depends on structurally: the
-- middleware checks codes that must exist, so seeding belongs in the migration
-- rather than in application startup. A code missing at runtime would fail
-- every request that needs it, and failing at migrate time is far cheaper.
--
-- Only capabilities that EXIST today are listed. Warehouse, product and
-- inventory permissions are deliberately absent — a permission for a module
-- that cannot be exercised is a promise the software does not keep, and it
-- would appear in every role editor as an option that does nothing.
-- ---------------------------------------------------------------------------

INSERT INTO permissions (id, code, name, module, created_at, updated_at) VALUES
    (gen_random_uuid(), 'company.read',            'View company',              'company',    NOW(), NOW()),
    (gen_random_uuid(), 'company.update',          'Update company',            'company',    NOW(), NOW()),
    (gen_random_uuid(), 'company.delete',          'Delete company',            'company',    NOW(), NOW()),

    (gen_random_uuid(), 'membership.read',         'View members',              'membership', NOW(), NOW()),
    (gen_random_uuid(), 'membership.invite',       'Invite members',            'membership', NOW(), NOW()),
    (gen_random_uuid(), 'membership.remove',       'Remove members',            'membership', NOW(), NOW()),

    (gen_random_uuid(), 'role.read',               'View roles',                'role',       NOW(), NOW()),
    (gen_random_uuid(), 'role.create',             'Create roles',              'role',       NOW(), NOW()),
    (gen_random_uuid(), 'role.update',             'Update roles',              'role',       NOW(), NOW()),
    (gen_random_uuid(), 'role.delete',             'Delete roles',              'role',       NOW(), NOW()),
    (gen_random_uuid(), 'role.assign_permissions', 'Assign role permissions',   'role',       NOW(), NOW()),

    (gen_random_uuid(), 'permission.read',         'View permission catalogue', 'permission', NOW(), NOW());
