-- 20260801100000: create_customers_table (up)
--
-- Customer is MASTER DATA owned by exactly one company — the structural sibling
-- of suppliers. A single-row aggregate with the postal address denormalised into
-- columns so an operator can filter or sort by city or country.

CREATE TABLE customers (
    id            UUID PRIMARY KEY,

    company_id    UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    -- The operator-facing identifier, printed on sales orders. CITEXT because
    -- "CUS-01" and "cus-01" are the same customer to a human keying an order.
    code          CITEXT       NOT NULL,

    name          VARCHAR(255) NOT NULL,

    email         CITEXT,
    phone         VARCHAR(32),
    tax_number    VARCHAR(64),

    address       TEXT         NOT NULL DEFAULT '',
    city          VARCHAR(128) NOT NULL DEFAULT '',
    province      VARCHAR(128) NOT NULL DEFAULT '',
    country       VARCHAR(128) NOT NULL DEFAULT '',
    postal_code   VARCHAR(16)  NOT NULL DEFAULT '',

    -- ACTIVE | INACTIVE. Created ACTIVE; deactivated to remove from selection for
    -- new orders while retaining history.
    status        VARCHAR(16)  NOT NULL DEFAULT 'ACTIVE',

    version       BIGINT       NOT NULL DEFAULT 1,

    created_by    UUID NOT NULL REFERENCES users (id),
    updated_by    UUID NOT NULL REFERENCES users (id),

    created_at    TIMESTAMPTZ  NOT NULL,
    updated_at    TIMESTAMPTZ  NOT NULL,
    deleted_at    TIMESTAMPTZ,

    CONSTRAINT customers_status_check
        CHECK (status IN ('ACTIVE', 'INACTIVE'))
);

-- Code is unique per company among live rows.
CREATE UNIQUE INDEX ux_customers_company_code
    ON customers (company_id, code)
    WHERE deleted_at IS NULL;

-- The operational hot path is "the ACTIVE customers of this company".
CREATE INDEX idx_customers_company_status
    ON customers (company_id, status)
    WHERE deleted_at IS NULL;
