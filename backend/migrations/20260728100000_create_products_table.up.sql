-- 20260728100000: create_products_table (up)
--
-- Product is an AGGREGATE ROOT (docs/Product.md). Its status reaches ACTIVE and
-- its tracking method changes only through domain methods, so this schema is a
-- persistence surface for a model that guards itself — not the guard.
--
-- The aggregate carries two child collections, barcodes and alternate units of
-- measure. They are stored in their own tables (product_barcodes, product_uoms)
-- rather than as JSONB columns for one decisive reason: a barcode must resolve
-- to exactly ONE product per company, and only a real UNIQUE index gives that a
-- race-proof guarantee. A JSONB array cannot carry a cross-row unique
-- constraint, so the scanner-correctness rule would degrade to an
-- application-level check that two concurrent inserts could both pass. See
-- docs/Product.md §4.

CREATE TABLE products (
    id             UUID PRIMARY KEY,

    company_id     UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    -- SKU is the operator-facing stock identifier: it is scanned, printed on
    -- pick lists and spoken over a radio. CITEXT because "SKU-1" and "sku-1"
    -- are the same article to a human, and letting both exist would make every
    -- stock conversation ambiguous.
    sku            CITEXT       NOT NULL,

    -- Name is ALSO unique per company. A product is chosen from a catalogue
    -- search by name, so two distinct articles both called "Blue Widget 500ml"
    -- would make mis-picking a matter of chance.
    name           CITEXT       NOT NULL,

    description    TEXT         NOT NULL DEFAULT '',

    -- category_id, brand_id and base_uom_id will reference future aggregates
    -- (Category, Brand, UOM). They are UUIDs with NO foreign key, because those
    -- tables do not exist yet and a constraint against a missing table cannot be
    -- written. The referential guarantee is application-level until then,
    -- enforced by the service's verifiers (docs/Product.md §5) — the same shape
    -- the warehouse sprint used for its zone ids.
    category_id    UUID,
    brand_id       UUID,

    -- base_uom_id is NOT NULL: every product is measured in exactly one base
    -- unit, provisioned at creation with conversion factor 1. It is the unit
    -- every alternate factor in product_uoms is expressed against.
    base_uom_id    UUID         NOT NULL,

    -- DRAFT | ACTIVE | DISCONTINUED. DISCONTINUED is terminal (docs/Product.md
    -- §3); there is no path out of it, which is why the table has no separate
    -- archive column — retirement is a status, not a soft delete.
    --
    -- CHECK rather than a PostgreSQL ENUM: adding a value to an ENUM needs
    -- ALTER TYPE, which complicates rollback for no benefit here.
    status         VARCHAR(16)  NOT NULL DEFAULT 'DRAFT',

    -- NONE | LOT | SERIAL. The aggregate refuses to change this once inventory
    -- exists; the column merely records the current choice.
    tracking       VARCHAR(16)  NOT NULL DEFAULT 'NONE',

    -- Shelf life in days. NULL means UNDEFINED, which is genuinely different
    -- from a defined zero-day shelf life (a product that expires on manufacture)
    -- — the aggregate models both, so a single nullable integer captures the
    -- distinction without a companion boolean: NULL = undefined, any value
    -- including 0 = defined.
    shelf_life_days INTEGER,

    -- Physical profile. Stored as TEXT holding the aggregate's canonical
    -- rational string ("333/1000", "1/3"), NOT as NUMERIC. The Product domain
    -- forbids float64 anywhere and represents every measurement as an exact
    -- big.Rat; NUMERIC(p,s) would round "1/3" and reintroduce exactly the
    -- rounding error the value objects exist to prevent. See docs/Product.md §6.
    --
    -- All nullable: a product may be registered before it is measured, and the
    -- aggregate distinguishes "no weight" from "zero weight".
    weight_kg      TEXT,
    volume_m3      TEXT,
    dim_width_cm   TEXT,
    dim_height_cm  TEXT,
    dim_length_cm  TEXT,

    -- Optimistic-lock token owned by the persistence layer (BaseEntity.Version).
    -- Included directly here rather than added by a later ALTER, because the
    -- table is new — the backfill dance in 20260727100000 was only for tables
    -- that predated the mechanism.
    version        BIGINT       NOT NULL DEFAULT 1,

    created_by     UUID NOT NULL REFERENCES users (id),
    updated_by     UUID NOT NULL REFERENCES users (id),

    created_at     TIMESTAMPTZ  NOT NULL,
    updated_at     TIMESTAMPTZ  NOT NULL,
    deleted_at     TIMESTAMPTZ,

    CONSTRAINT products_status_check
        CHECK (status IN ('DRAFT', 'ACTIVE', 'DISCONTINUED')),
    CONSTRAINT products_tracking_check
        CHECK (tracking IN ('NONE', 'LOT', 'SERIAL')),
    -- Shelf life may be zero (expires on manufacture) but never negative, which
    -- would make every expiry calculation meaningless.
    CONSTRAINT products_shelf_life_check
        CHECK (shelf_life_days IS NULL OR shelf_life_days >= 0)
);

-- SKU and name are BOTH unique per company, among live rows. Partial on
-- deleted_at so a discontinued-then-purged SKU could be reused; products are
-- not soft-deleted today, but the predicate keeps the index consistent with
-- every other table's and costs nothing.
CREATE UNIQUE INDEX ux_products_company_sku
    ON products (company_id, sku)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX ux_products_company_name
    ON products (company_id, name)
    WHERE deleted_at IS NULL;

-- Every product query is company-scoped, so company_id leads. Status is carried
-- because the operational hot path is "the ACTIVE products of this company" —
-- the set a receiving or picking screen offers.
CREATE INDEX idx_products_company_status
    ON products (company_id, status)
    WHERE deleted_at IS NULL;

-- ============================================================================
-- product_barcodes: the barcode child collection.
-- ============================================================================
--
-- A separate table, not a JSONB column, because a barcode must resolve to one
-- product per company and that needs a real UNIQUE index (below).
--
-- These rows have no independent lifecycle: they are written and replaced
-- wholesale with their parent product inside one transaction, and hard-deleted
-- on replacement rather than soft-deleted — the aggregate and its events are the
-- audit trail, not the child rows. So there is no version or deleted_at here.
CREATE TABLE product_barcodes (
    id          UUID PRIMARY KEY,

    -- CASCADE: a barcode cannot outlive the product it identifies. Unlike a
    -- warehouse, a product row genuinely belongs to its parent aggregate, so
    -- removing the parent must remove these.
    product_id  UUID NOT NULL REFERENCES products (id) ON DELETE CASCADE,

    -- Denormalised onto the row so the cross-product unique index below can be
    -- company-scoped without a join to products on every insert.
    company_id  UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    barcode     CITEXT   NOT NULL,

    -- Exactly one row per product carries is_primary = TRUE. That invariant is
    -- enforced by the AGGREGATE, not by the table — a partial unique index on
    -- (product_id) WHERE is_primary could enforce "at most one", but not "at
    -- least one", and the aggregate already guarantees both. See docs/Product.md.
    is_primary  BOOLEAN  NOT NULL DEFAULT FALSE,

    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

-- The scanner-correctness guarantee: one barcode resolves to one product within
-- a company. CITEXT so a lower-case scan cannot smuggle in a duplicate.
CREATE UNIQUE INDEX ux_product_barcodes_company_barcode
    ON product_barcodes (company_id, barcode);

-- Loading a product's barcodes, and replacing them on update, both filter by
-- product_id.
CREATE INDEX idx_product_barcodes_product
    ON product_barcodes (product_id);

-- ============================================================================
-- product_uoms: the alternate-unit-of-measure child collection.
-- ============================================================================
--
-- Same storage rationale as barcodes: a real table so (product_id, uom_id)
-- uniqueness is a DB guarantee, and so the conversion factor is a first-class
-- column rather than buried in JSON.
CREATE TABLE product_uoms (
    id          UUID PRIMARY KEY,

    product_id  UUID NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    company_id  UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    -- References the future UOM aggregate. No foreign key yet, for the same
    -- reason as products.base_uom_id.
    uom_id      UUID NOT NULL,

    -- The exact conversion factor to the base unit, as the aggregate's canonical
    -- rational string. TEXT not NUMERIC, for the exactness reason above: a
    -- factor of one third is "1/3", and NUMERIC would round it — after which
    -- every quantity conversion derived from it would be wrong.
    factor      TEXT     NOT NULL,

    -- Marks the base unit's own row (factor 1). Carried so a reader can identify
    -- the base without comparing against products.base_uom_id.
    is_base     BOOLEAN  NOT NULL DEFAULT FALSE,

    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

-- A unit may appear at most once per product: two rows for the same uom_id
-- would give one unit two conflicting conversion factors.
CREATE UNIQUE INDEX ux_product_uoms_product_uom
    ON product_uoms (product_id, uom_id);

CREATE INDEX idx_product_uoms_product
    ON product_uoms (product_id);
