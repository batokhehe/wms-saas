-- 20260726100000: create_storage_locations_table (up)
--
-- A storage location is a physical place inside a warehouse where inventory can
-- exist: a bin, a rack level, a floor position, a receiving dock.
--
-- It is a SEPARATE AGGREGATE from Warehouse, not a child collection of it. A
-- large distribution centre has tens of thousands of locations, so loading a
-- warehouse "with its locations" would be unusable — and locking a warehouse to
-- change one bin's capacity would serialise the whole site. The reference is by
-- id in one direction only, which is the standard rule for aggregate design.

CREATE TABLE storage_locations (
    id               UUID PRIMARY KEY,

    company_id       UUID NOT NULL REFERENCES companies (id)   ON DELETE CASCADE,

    -- The owning warehouse. RESTRICT rather than CASCADE: a warehouse is never
    -- hard-deleted (docs/Warehouse.md §5), so a cascade here would only ever
    -- fire during an administrative purge — and silently destroying tens of
    -- thousands of location rows is not something a purge should do implicitly.
    warehouse_id     UUID NOT NULL REFERENCES warehouses (id)  ON DELETE RESTRICT,

    -- The operator-facing identifier, printed on rack labels and spoken over a
    -- radio: "A-01-02-03". CITEXT because "a-01" and "A-01" are the same
    -- location to a human holding a scanner.
    code             CITEXT       NOT NULL,

    -- The structured coordinate. Denormalised into columns rather than kept
    -- only inside `code`, because picking routes are optimised by sorting on
    -- aisle then rack then level — and parsing a string in an ORDER BY would
    -- make that impossible to index.
    --
    -- Zone is required; the rest are optional, because a floor-stack location
    -- has a zone and nothing else.
    zone             VARCHAR(32)  NOT NULL,
    aisle            VARCHAR(32)  NOT NULL DEFAULT '',
    rack             VARCHAR(32)  NOT NULL DEFAULT '',
    level            VARCHAR(32)  NOT NULL DEFAULT '',
    bin              VARCHAR(32)  NOT NULL DEFAULT '',

    -- Nullable: a location may be labelled later, or never. It is unique per
    -- COMPANY rather than per warehouse, because a scanner reads a barcode with
    -- no idea which warehouse it is standing in — a duplicate across two sites
    -- would make the scan ambiguous exactly when it matters.
    barcode          VARCHAR(64),

    -- ACTIVE | INACTIVE | LOCKED | MAINTENANCE.
    status           VARCHAR(16)  NOT NULL DEFAULT 'ACTIVE',

    -- Lower sorts first when building a pick path. An integer rather than a
    -- float so two locations cannot be "almost" equal in priority.
    picking_priority INTEGER      NOT NULL DEFAULT 100,

    -- Whether more than one SKU may occupy this location at once. False is the
    -- safer default: a mixed bin is a picking-error source, and a business that
    -- wants them should opt in per location.
    allow_mixed_sku  BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Whether putaway may exceed the declared capacity. Also false by default —
    -- an overflowing rack is a safety issue, not a convenience.
    allow_overflow   BOOLEAN      NOT NULL DEFAULT FALSE,

    -- Capacity limits. All nullable, because NULL means "not measured" and is
    -- genuinely different from zero: an unmeasured bin accepts stock, a
    -- zero-capacity bin accepts none.
    --
    -- NUMERIC rather than double precision. A WMS adds and subtracts these
    -- values thousands of times a day, and binary floating point accumulates
    -- error on every operation — the discrepancy surfaces as a capacity check
    -- that passes when it should fail. See docs/EntityConvention.md §6.
    max_weight       NUMERIC(14, 3),
    max_volume       NUMERIC(14, 3),
    max_pallet       INTEGER,

    created_by       UUID NOT NULL REFERENCES users (id),
    updated_by       UUID NOT NULL REFERENCES users (id),

    created_at       TIMESTAMPTZ  NOT NULL,
    updated_at       TIMESTAMPTZ  NOT NULL,
    deleted_at       TIMESTAMPTZ,

    CONSTRAINT storage_locations_status_check
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'LOCKED', 'MAINTENANCE')),

    -- Capacity may be zero (a location deliberately taken out of use by
    -- capacity) but never negative, which would make every comparison against
    -- it meaningless.
    CONSTRAINT storage_locations_capacity_check
        CHECK (
            (max_weight IS NULL OR max_weight >= 0) AND
            (max_volume IS NULL OR max_volume >= 0) AND
            (max_pallet IS NULL OR max_pallet >= 0)
        )
);

-- Code is unique within a WAREHOUSE, not within a company.
--
-- Two sites both having an "A-01-01-01" is normal and expected — the aisle
-- numbering restarts at every building. Making it company-unique would force
-- operators to prefix every label with a site code they can already see.
CREATE UNIQUE INDEX ux_storage_locations_warehouse_code
    ON storage_locations (warehouse_id, code)
    WHERE deleted_at IS NULL;

-- Barcode is unique within a COMPANY, and only where one is set.
--
-- The partial predicate carries two conditions: NULL barcodes are exempt (many
-- locations have none, and NULLs do not collide in PostgreSQL anyway — stated
-- explicitly so the intent is not mistaken for an oversight), and archived
-- rows release their barcode for reuse on a replacement label.
CREATE UNIQUE INDEX ux_storage_locations_company_barcode
    ON storage_locations (company_id, barcode)
    WHERE deleted_at IS NULL AND barcode IS NOT NULL;

-- The operational hot path is "the usable locations in this warehouse", which
-- every putaway and pick query starts from. company_id leads, per the
-- tenant-scoping rule in docs/MigrationGuide.md §6.
CREATE INDEX idx_storage_locations_company_warehouse_status
    ON storage_locations (company_id, warehouse_id, status)
    WHERE deleted_at IS NULL;

-- Pick-path ordering. A picker walks aisle by aisle, so a route is built by
-- sorting on priority then coordinate — and without this index that sort is a
-- full scan of every location in the site.
CREATE INDEX idx_storage_locations_pick_path
    ON storage_locations (warehouse_id, picking_priority, aisle, rack, level, bin)
    WHERE deleted_at IS NULL;
