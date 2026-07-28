-- 20260722100000: create_users_table (up)
--
-- The users table holds identity only. It carries NO company_id, and that is a
-- deliberate architectural decision rather than a gap to be filled later:
--
--   * A person can belong to several companies. In a warehouse SaaS a 3PL
--     operator legitimately works for multiple clients, and a logistics manager
--     may oversee two subsidiaries. Putting company_id here would force one row
--     per company per person, which means duplicate credentials, duplicate
--     password resets and no single account to lock when someone leaves.
--   * Authentication must work before a company context exists. Login happens,
--     then the user selects or is assigned a company.
--
-- The user-to-company relationship becomes a separate memberships table in
-- Sprint 2. Nothing in this migration needs to change for that to happen.

CREATE TABLE users (
    id                UUID PRIMARY KEY,

    -- CITEXT so "Ops@example.com" and "ops@example.com" are the same account.
    -- Storing this as VARCHAR would let one person register twice with the same
    -- address in different cases, and then be unable to explain why their
    -- password "stopped working".
    email             CITEXT      NOT NULL,

    -- bcrypt output is always 60 ASCII characters. Sized exactly so a bug that
    -- writes a raw password here fails on length rather than storing it.
    password_hash     VARCHAR(60) NOT NULL,

    full_name         VARCHAR(255) NOT NULL,

    -- ACTIVE | INACTIVE | LOCKED. A CHECK constraint rather than a PostgreSQL
    -- ENUM type: adding a value to an ENUM requires ALTER TYPE, which cannot
    -- run inside a transaction in older versions and complicates rollback.
    status            VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',

    last_login_at     TIMESTAMPTZ,
    email_verified_at TIMESTAMPTZ,

    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    deleted_at        TIMESTAMPTZ,

    CONSTRAINT users_status_check CHECK (status IN ('ACTIVE', 'INACTIVE', 'LOCKED'))
);

-- Partial unique index: the constraint applies only to live rows.
--
-- A plain UNIQUE would let a deleted account permanently reserve its email
-- address, so a person who deleted their account could never sign up again with
-- the same address. See docs/SoftDeleteConvention.md.
CREATE UNIQUE INDEX ux_users_email ON users (email) WHERE deleted_at IS NULL;

-- Login looks up by email and immediately filters on status, so the index
-- carries status to allow an index-only scan on the hot path.
CREATE INDEX idx_users_status ON users (status) WHERE deleted_at IS NULL;
