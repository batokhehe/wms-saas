-- 20260722100001: create_refresh_tokens_table (up)
--
-- Refresh tokens are stored as SHA-256 hashes, never as the value handed to the
-- client. A database leak therefore yields no usable session: the attacker
-- holds digests, and the tokens themselves are 256 bits of randomness that
-- cannot be recovered from them.
--
-- Why SHA-256 rather than bcrypt, when passwords use bcrypt:
--
--   * bcrypt is deliberately slow to defend LOW-entropy secrets. A password is
--     guessable; a 256-bit random token is not, so there is nothing to slow an
--     attacker down against.
--   * Refresh happens on a hot path and must look the token up by its hash.
--     bcrypt embeds a per-hash salt, so the digest is not reproducible from the
--     input — the only way to find a match would be to scan every row and
--     bcrypt-compare each one. SHA-256 is deterministic, so the lookup is a
--     single indexed equality.

CREATE TABLE refresh_tokens (
    id           UUID PRIMARY KEY,

    user_id      UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- SHA-256 rendered as lowercase hex: always exactly 64 characters.
    token_hash   CHAR(64) NOT NULL,

    expires_at   TIMESTAMPTZ NOT NULL,

    -- Set when the token is rotated, on logout, or when reuse is detected.
    -- This is the token's lifecycle state and is distinct from deleted_at,
    -- which means erasure. See docs/SoftDeleteConvention.md.
    revoked_at   TIMESTAMPTZ,

    -- Session provenance, for the "your active sessions" view and for
    -- investigating a compromised account. Free text supplied by the client, so
    -- it is untrusted and length-bounded.
    device       VARCHAR(255),
    ip_address   INET,
    user_agent   VARCHAR(512),

    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL,
    deleted_at   TIMESTAMPTZ,

    -- A hash collision would let one session authenticate as another, so
    -- uniqueness is enforced by the database and not merely assumed from the
    -- generator.
    CONSTRAINT ux_refresh_tokens_hash UNIQUE (token_hash)
);

-- The refresh hot path is: find by hash, check not revoked, check not expired.
CREATE INDEX idx_refresh_tokens_hash_live ON refresh_tokens (token_hash)
    WHERE revoked_at IS NULL AND deleted_at IS NULL;

-- Listing and mass-revoking a user's sessions (logout-everywhere, and the
-- reuse-detection response) filter by user_id.
CREATE INDEX idx_refresh_tokens_user ON refresh_tokens (user_id)
    WHERE deleted_at IS NULL;

-- The cleanup job deletes expired tokens; it scans by expires_at.
CREATE INDEX idx_refresh_tokens_expires ON refresh_tokens (expires_at)
    WHERE deleted_at IS NULL;
