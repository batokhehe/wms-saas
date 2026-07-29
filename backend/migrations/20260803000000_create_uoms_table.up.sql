CREATE TABLE uoms (
    id UUID PRIMARY KEY,
    code CITEXT NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ,
    CONSTRAINT uoms_status_check CHECK (status IN ('ACTIVE', 'INACTIVE'))
);
CREATE UNIQUE INDEX ux_uoms_code ON uoms (code) WHERE deleted_at IS NULL;
CREATE INDEX idx_uoms_status ON uoms (status) WHERE deleted_at IS NULL;
