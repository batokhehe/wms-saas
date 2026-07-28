-- 20260731100000: create_suppliers_table (up)
--
-- Supplier is MASTER DATA owned by exactly one company. A single-row aggregate:
-- one table, persisted with an optimistic-lock version. The postal address is a
-- single Address value object in the domain but is denormalised into columns
-- here so an operator can filter or sort by city or country.

CREATE TABLE suppliers (
    id            UUID PRIMARY KEY,

    company_id    UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    -- The operator-facing identifier, printed on purchase orders. CITEXT because
    -- "SUP-01" and "sup-01" are the same supplier to a human keying an order.
    code          CITEXT       NOT NULL,

    name          VARCHAR(255) NOT NULL,

    -- Optional contact and registration details. All nullable: a supplier may be
    -- onboarded before they are captured.
    email         CITEXT,
    phone         VARCHAR(32),
    tax_number    VARCHAR(64),

    address       TEXT         NOT NULL DEFAULT '',
    city          VARCHAR(128) NOT NULL DEFAULT '',
    province      VARCHAR(128) NOT NULL DEFAULT '',
    country       VARCHAR(128) NOT NULL DEFAULT '',
    postal_code   VARCHAR(16)  NOT NULL DEFAULT '',

    -- ACTIVE | INACTIVE. A supplier is created ACTIVE and deactivated to remove
    -- it from selection for new orders while retaining its history.
    status        VARCHAR(16)  NOT NULL DEFAULT 'ACTIVE',

    version       BIGINT       NOT NULL DEFAULT 1,

    created_by    UUID NOT NULL REFERENCES users (id),
    updated_by    UUID NOT NULL REFERENCES users (id),

    created_at    TIMESTAMPTZ  NOT NULL,
    updated_at    TIMESTAMPTZ  NOT NULL,
    deleted_at    TIMESTAMPTZ,

    CONSTRAINT suppliers_status_check
        CHECK (status IN ('ACTIVE', 'INACTIVE'))
);

-- Code is unique per company among live rows. Partial on deleted_at for
-- consistency with every other table, though suppliers are not deleted today.
CREATE UNIQUE INDEX ux_suppliers_company_code
    ON suppliers (company_id, code)
    WHERE deleted_at IS NULL;

-- Every supplier query is company-scoped, so company_id leads. Status is carried
-- because the operational hot path is "the ACTIVE suppliers of this company" —
-- the set a purchase-order screen offers.
CREATE INDEX idx_suppliers_company_status
    ON suppliers (company_id, status)
    WHERE deleted_at IS NULL;
