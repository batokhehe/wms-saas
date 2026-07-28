-- 20260723100001: create_memberships_table (up)
--
-- Memberships is the join between users and companies, and it is the ONLY link
-- between them. users has no company_id and companies has no owner_id, so this
-- table is the single source of truth for "who belongs to what".
--
-- Why the relationship is many-to-many rather than a column on users:
--
--   * A person legitimately belongs to several companies. A 3PL operator works
--     for multiple clients; a manager oversees two subsidiaries. A company_id
--     on users would force one account per company — duplicate credentials,
--     duplicate password resets, and no single account to lock when that person
--     leaves.
--   * Role is a property of the RELATIONSHIP, not of the person. The same human
--     can be OWNER of their own company and STAFF at a client's. A role column
--     on users could not express that.

CREATE TABLE memberships (
    id          UUID PRIMARY KEY,

    -- ON DELETE CASCADE on both sides: a membership has no meaning once either
    -- end is gone. Note this only fires on a HARD delete; ordinary deletion is
    -- soft, so the row survives with deleted_at set and history stays intact.
    company_id  UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users (id)     ON DELETE CASCADE,

    -- OWNER | ADMIN | STAFF. Stored but NOT enforced anywhere yet: RBAC is the
    -- next sprint. The column exists now so that enabling permission checks is
    -- a change to the authorisation layer rather than a migration on a
    -- populated table.
    role        VARCHAR(16) NOT NULL,

    -- ACTIVE | PENDING | SUSPENDED.
    --
    -- PENDING is what makes invitation work: the row exists (so the invite is
    -- durable and the seat is reserved) but the membership cannot be used to
    -- access anything until accepted. Only ACTIVE memberships resolve a company
    -- context.
    status      VARCHAR(16) NOT NULL DEFAULT 'PENDING',

    -- Set when the membership becomes ACTIVE. NULL while PENDING, which is why
    -- it is nullable rather than defaulting to created_at: "invited on Monday,
    -- joined on Friday" is a real and auditable distinction.
    joined_at   TIMESTAMPTZ,

    -- Who issued the invitation. NULL for the OWNER membership created during
    -- company registration, because nobody invited the founder.
    --
    -- ON DELETE SET NULL rather than CASCADE: deleting the person who sent an
    -- invitation must not delete the membership of the person they invited.
    invited_by  UUID REFERENCES users (id) ON DELETE SET NULL,

    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    deleted_at  TIMESTAMPTZ,

    CONSTRAINT memberships_role_check
        CHECK (role IN ('OWNER', 'ADMIN', 'STAFF')),
    CONSTRAINT memberships_status_check
        CHECK (status IN ('ACTIVE', 'PENDING', 'SUSPENDED'))
);

-- One membership per user per company, among live rows.
--
-- This is the integrity rule that makes company resolution deterministic:
-- without it a user could hold two memberships in one company with different
-- roles, and "what is this user's role here?" would have no single answer.
--
-- Partial on deleted_at so a removed member can be re-invited later.
CREATE UNIQUE INDEX ux_memberships_company_user
    ON memberships (company_id, user_id)
    WHERE deleted_at IS NULL;

-- The company-context middleware runs on EVERY authenticated request and looks
-- up (user_id, company_id) to resolve the active tenant. This composite index
-- is what keeps that lookup off the critical path.
CREATE INDEX idx_memberships_user_company
    ON memberships (user_id, company_id)
    WHERE deleted_at IS NULL;

-- "Which companies can I switch to?" filters by user and status.
CREATE INDEX idx_memberships_user_status
    ON memberships (user_id, status)
    WHERE deleted_at IS NULL;

-- "Who is in this company?" — the member list endpoint. company_id leads,
-- matching the tenant-scoping rule in docs/MigrationGuide.md §6.
CREATE INDEX idx_memberships_company
    ON memberships (company_id)
    WHERE deleted_at IS NULL;
