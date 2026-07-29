CREATE TABLE brands (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL REFERENCES companies(id),
    code CITEXT NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE UNIQUE INDEX ux_brands_company_code ON brands(company_id, code) WHERE deleted_at IS NULL;
CREATE INDEX ix_brands_company_status ON brands(company_id, status) WHERE deleted_at IS NULL;
