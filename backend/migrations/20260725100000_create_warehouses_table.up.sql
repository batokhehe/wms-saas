-- 20260725100000: create_warehouses_table (up)
--
-- The first business domain table. Warehouse is an AGGREGATE ROOT: every future
-- operational concept — zones, locations, stock, receipts, shipments — will
-- reference a warehouse, and none of them may be reached except through it.
--
-- The schema reflects that. Columns that will become foreign keys into future
-- aggregates are nullable UUIDs with NO constraint yet, because a constraint
-- against a table that does not exist cannot be written and a placeholder table
-- would be worse than nothing.

CREATE TABLE warehouses (
    id                        UUID PRIMARY KEY,

    company_id                UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    -- Code is the operator-facing identifier: it appears on picking lists,
    -- labels and transfer documents. CITEXT because "WH-01" and "wh-01" are the
    -- same warehouse to a human holding a scanner, and letting both exist would
    -- make every support conversation ambiguous.
    code                      CITEXT       NOT NULL,

    -- Name is ALSO unique per company, which is unusual and deliberate: an
    -- operator picks a destination warehouse from a dropdown by name, not by
    -- code, so two warehouses called "Jakarta Central" would make mis-shipping
    -- a matter of chance rather than error.
    name                      CITEXT       NOT NULL,

    description               TEXT         NOT NULL DEFAULT '',

    -- MAIN | BRANCH | TRANSIT | CONSIGNMENT.
    -- CHECK rather than a PostgreSQL ENUM: adding a value to an ENUM requires
    -- ALTER TYPE, which complicates rollback for no benefit here.
    type                      VARCHAR(16)  NOT NULL,

    -- DRAFT | ACTIVE | INACTIVE | SUSPENDED.
    --
    -- DRAFT is the creation state and is why the table has no NOT NULL on the
    -- address and contact columns: a warehouse is registered before its details
    -- are known, and only becomes ACTIVE once they are. Enforcing completeness
    -- at the column level would make the DRAFT state impossible to represent.
    status                    VARCHAR(16)  NOT NULL DEFAULT 'DRAFT',

    address                   TEXT         NOT NULL DEFAULT '',
    contact_name              VARCHAR(255) NOT NULL DEFAULT '',
    contact_phone             VARCHAR(32)  NOT NULL DEFAULT '',

    -- Operational zone assignments.
    --
    -- These will reference the future zones/locations aggregate. They are
    -- nullable UUIDs with NO foreign key, because that table does not exist —
    -- and inventing a placeholder for it would bake in a shape the Location
    -- sprint has not yet chosen.
    --
    -- The referential guarantee is therefore application-level until then. That
    -- is stated plainly rather than hidden: see docs/Warehouse.md §7.
    default_receiving_zone_id UUID,
    default_shipping_zone_id  UUID,
    default_staging_zone_id   UUID,

    -- Audit columns distinct from created_at/updated_at: "when" and "who" are
    -- different questions, and a warehouse suspension is exactly the kind of
    -- event where the second one matters.
    created_by                UUID NOT NULL REFERENCES users (id),
    updated_by                UUID NOT NULL REFERENCES users (id),

    created_at                TIMESTAMPTZ  NOT NULL,
    updated_at                TIMESTAMPTZ  NOT NULL,

    -- A warehouse is NEVER hard-deleted. Archiving sets this column; the row and
    -- every future stock movement referencing it stay intact. See
    -- docs/SoftDeleteConvention.md and docs/Warehouse.md §5.
    deleted_at                TIMESTAMPTZ,

    CONSTRAINT warehouses_type_check
        CHECK (type IN ('MAIN', 'BRANCH', 'TRANSIT', 'CONSIGNMENT')),
    CONSTRAINT warehouses_status_check
        CHECK (status IN ('DRAFT', 'ACTIVE', 'INACTIVE', 'SUSPENDED'))
);

-- Code and name are BOTH unique per company, among live rows.
--
-- Partial on deleted_at so an archived warehouse's code can be reused — a
-- business closes a site and later opens a new one on the same code, and a
-- permanent reservation would force them to invent "WH-01-NEW".
CREATE UNIQUE INDEX ux_warehouses_company_code
    ON warehouses (company_id, code)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX ux_warehouses_company_name
    ON warehouses (company_id, name)
    WHERE deleted_at IS NULL;

-- Every warehouse query is company-scoped, so company_id leads. Status is
-- carried because the operational hot path is "the ACTIVE warehouses of this
-- company" — the set a receiving or shipping screen offers.
CREATE INDEX idx_warehouses_company_status
    ON warehouses (company_id, status)
    WHERE deleted_at IS NULL;
