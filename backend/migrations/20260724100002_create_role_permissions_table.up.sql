-- 20260724100002: create_role_permissions_table (up)
--
-- The grant table: which permissions a role holds. This is where per-tenant
-- authorisation actually lives — permissions are global, roles are per-company,
-- and the MAPPING between them is what each company controls.
--
-- It carries no company_id of its own. The tenant is implied by role_id, and
-- duplicating it here would create a second source of truth that could
-- disagree with roles.company_id — a denormalisation whose only possible
-- outcome is a grant pointing at the wrong tenant. Every query joins through
-- roles, which is already company-scoped.

CREATE TABLE role_permissions (
    id            UUID PRIMARY KEY,

    role_id       UUID NOT NULL REFERENCES roles (id)       ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions (id) ON DELETE CASCADE,

    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    deleted_at    TIMESTAMPTZ
);

-- One grant per (role, permission) among live rows.
--
-- Partial on deleted_at so a revoked permission can be re-granted. Soft delete
-- is deliberate here rather than a hard DELETE: "who revoked what, and when" is
-- exactly the question an access-control audit asks, and a hard delete leaves
-- no evidence it was ever granted.
CREATE UNIQUE INDEX ux_role_permissions_role_permission
    ON role_permissions (role_id, permission_id)
    WHERE deleted_at IS NULL;

-- The evaluator resolves a role's whole permission set on every request, so
-- role_id leads.
CREATE INDEX idx_role_permissions_role
    ON role_permissions (role_id)
    WHERE deleted_at IS NULL;

-- "Which roles grant this permission?" — used when a permission is deprecated.
CREATE INDEX idx_role_permissions_permission
    ON role_permissions (permission_id)
    WHERE deleted_at IS NULL;
