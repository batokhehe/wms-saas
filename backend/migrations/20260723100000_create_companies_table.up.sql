-- 20260723100000: create_companies_table (up)
--
-- A company IS the tenant, so this table carries no company_id of its own.
-- Every OTHER tenant-owned table will reference companies(id); this one is the
-- root of that graph.
--
-- Note also what is absent: no owner_id, no user_id. The link between a company
-- and the people in it lives entirely in memberships (the next migration).
-- Putting an owner_id here would create a second, competing source of truth for
-- "who runs this company" that could disagree with the OWNER membership row.

CREATE TABLE companies (
    id          UUID PRIMARY KEY,

    -- Code is the human-facing tenant identifier ("ACME", "WH-JAKARTA-01"). It
    -- appears on documents and in support conversations, so it must be stable
    -- and unambiguous. CITEXT because "acme" and "ACME" are the same company —
    -- storing it as VARCHAR would let two tenants claim visually identical
    -- codes and make every support ticket ambiguous.
    code        CITEXT       NOT NULL,

    name        VARCHAR(255) NOT NULL,

    -- Contact details are optional: a company is created during onboarding,
    -- before the operator has necessarily supplied them. Requiring them here
    -- would block the register flow on data nobody has yet.
    email       CITEXT,
    phone       VARCHAR(32),

    -- Logo is an object-store KEY, never a URL and never image bytes. The
    -- bucket and CDN host are deployment concerns that change independently of
    -- the data; a stored URL would break the day the CDN domain changes, and
    -- bytes in a row would bloat every SELECT that touches the company.
    logo        VARCHAR(512),

    address     TEXT,

    -- ACTIVE | INACTIVE | SUSPENDED. A CHECK constraint rather than a
    -- PostgreSQL ENUM: adding a value to an ENUM requires ALTER TYPE, which
    -- complicates rollback for no benefit here.
    status      VARCHAR(16)  NOT NULL DEFAULT 'ACTIVE',

    created_at  TIMESTAMPTZ  NOT NULL,
    updated_at  TIMESTAMPTZ  NOT NULL,
    deleted_at  TIMESTAMPTZ,

    CONSTRAINT companies_status_check
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'SUSPENDED'))
);

-- Partial unique index: the constraint applies only to live rows.
--
-- A plain UNIQUE would let a deleted company permanently reserve its code, so a
-- tenant who deleted "ACME" could never re-create it. See
-- docs/SoftDeleteConvention.md.
CREATE UNIQUE INDEX ux_companies_code ON companies (code) WHERE deleted_at IS NULL;

-- Listing companies filters on status.
CREATE INDEX idx_companies_status ON companies (status) WHERE deleted_at IS NULL;
