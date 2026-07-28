-- 20260724100001: create_roles_table (up)
--
-- Roles ARE tenant-owned: each company has its own OWNER/ADMIN/STAFF rows plus
-- any custom roles it defines. Two companies may grant completely different
-- permissions to the role they both call "ADMIN", and neither can see the
-- other's definition.
--
-- # How this joins to memberships without changing it
--
-- memberships.role already stores a role NAME ('OWNER', 'ADMIN', 'STAFF') as a
-- varchar. This table's (company_id, name) pair is what that string resolves
-- against:
--
--     membership(company_id, role) ──► roles(company_id, name) ──► permissions
--
-- No foreign key is declared from memberships to roles, and none is added:
-- doing so would require altering the memberships table, which this sprint must
-- not touch. The join is by name within a company, enforced by the unique index
-- below rather than by a constraint on the other table.
--
-- The trade-off is explicit: renaming a role would orphan the memberships that
-- name it, so RoleService refuses to rename a system role and the API does not
-- expose a rename for custom ones. See docs/RBAC.md.

CREATE TABLE roles (
    id          UUID PRIMARY KEY,

    company_id  UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    -- Name is the join key against memberships.role, so it is compared exactly.
    -- CITEXT would make "admin" and "ADMIN" the same row, which is right for a
    -- human-entered identifier and prevents a company creating two roles that
    -- look identical in a picker.
    name        CITEXT       NOT NULL,

    description VARCHAR(255) NOT NULL DEFAULT '',

    -- IsSystem marks OWNER/ADMIN/STAFF. System roles are provisioned by the
    -- application, cannot be deleted, and cannot be renamed — memberships point
    -- at them by name, so removing one would strand every member holding it
    -- with no way to re-grant.
    --
    -- Their PERMISSIONS remain editable: a company legitimately wants to decide
    -- what its own ADMIN can do. It is the role's existence and identity that
    -- are protected, not its contents.
    is_system   BOOLEAN      NOT NULL DEFAULT FALSE,

    created_at  TIMESTAMPTZ  NOT NULL,
    updated_at  TIMESTAMPTZ  NOT NULL,
    deleted_at  TIMESTAMPTZ
);

-- One role per name per company, among live rows.
--
-- This is what makes name-based resolution deterministic: without it a company
-- could hold two roles called "ADMIN" with different permissions, and "what can
-- an ADMIN do here?" would have no single answer.
--
-- Partial on deleted_at so a deleted custom role's name can be reused.
CREATE UNIQUE INDEX ux_roles_company_name
    ON roles (company_id, name)
    WHERE deleted_at IS NULL;

-- The permission evaluator runs on every authorised request and looks up
-- (company_id, name). company_id leads, per the tenant-scoping rule in
-- docs/MigrationGuide.md §6.
CREATE INDEX idx_roles_company ON roles (company_id) WHERE deleted_at IS NULL;
