-- 20260729100000: create_inventories_table (up)
--
-- Inventory is an AGGREGATE ROOT: the current state of ONE product inside ONE
-- storage location. It is a single-row aggregate — no child collections — so
-- this is one table, persisted atomically with an optimistic-lock version.
--
-- The stock quantities live here and NOWHERE else: the aggregate owns every
-- transition, and this schema is the surface for a model that guards itself. The
-- CHECK constraints below are backstops that re-state the aggregate's invariants
-- at the database level, so a write that reached the table another way still
-- cannot store a nonsensical position.

CREATE TABLE inventories (
    id            UUID PRIMARY KEY,

    company_id    UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    -- The physical location of the stock. RESTRICT rather than CASCADE on all
    -- three: a warehouse, location or product that still has stock against it
    -- must not be hard-deleted out from under its inventory — the same rule
    -- storage_locations applies to its warehouse (docs/StorageLocation.md).
    warehouse_id  UUID NOT NULL REFERENCES warehouses (id)         ON DELETE RESTRICT,
    location_id   UUID NOT NULL REFERENCES storage_locations (id)  ON DELETE RESTRICT,
    product_id    UUID NOT NULL REFERENCES products (id)           ON DELETE RESTRICT,

    -- NONE | LOT | SERIAL. How the stock is individuated. NONE is a fungible
    -- pool; LOT is one record per batch; SERIAL is one record per unit.
    tracking_type VARCHAR(16) NOT NULL,

    -- Present only for the tracking type that requires them. CITEXT because a
    -- scanned lot or serial is the same identifier whatever its case.
    lot_number    CITEXT,
    serial_number CITEXT,

    -- Integer counts of base units. BIGINT, not NUMERIC: these are discrete
    -- counts, and integer arithmetic is exact and overflow-checked in the
    -- aggregate. Reserved is what is promised; available (on_hand - reserved) is
    -- derived and never stored.
    on_hand       BIGINT NOT NULL DEFAULT 0,
    reserved      BIGINT NOT NULL DEFAULT 0,

    -- ACTIVE | LOCKED. LOCKED freezes every quantity movement.
    status        VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',

    -- Optimistic-lock token, owned by the persistence layer.
    version       BIGINT NOT NULL DEFAULT 1,

    created_by    UUID NOT NULL REFERENCES users (id),
    updated_by    UUID NOT NULL REFERENCES users (id),

    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    deleted_at    TIMESTAMPTZ,

    CONSTRAINT inventories_tracking_check
        CHECK (tracking_type IN ('NONE', 'LOT', 'SERIAL')),
    CONSTRAINT inventories_status_check
        CHECK (status IN ('ACTIVE', 'LOCKED')),

    -- The core quantity invariants: non-negative, and you cannot promise more
    -- than you hold.
    CONSTRAINT inventories_quantities_check
        CHECK (on_hand >= 0 AND reserved >= 0 AND on_hand >= reserved),

    -- The lot/serial presence rules per tracking type, and the serial
    -- fixed-quantity rule. This is the database backstop for what
    -- entity.validateTracking enforces in the domain.
    CONSTRAINT inventories_tracking_presence_check
        CHECK (
            (tracking_type = 'NONE'   AND lot_number IS NULL     AND serial_number IS NULL) OR
            (tracking_type = 'LOT'    AND lot_number IS NOT NULL AND serial_number IS NULL) OR
            (tracking_type = 'SERIAL' AND serial_number IS NOT NULL AND lot_number IS NULL AND on_hand <= 1)
        )
);

-- ---------------------------------------------------------------------------
-- Uniqueness (one authoritative record per addressable stock position)
-- ---------------------------------------------------------------------------
--
-- Each is PARTIAL on its tracking type (and on deleted_at IS NULL, consistent
-- with every other table), so the three rules never collide with one another.

-- NONE: exactly one fungible pool per product per location.
CREATE UNIQUE INDEX ux_inventories_none_position
    ON inventories (company_id, product_id, location_id)
    WHERE tracking_type = 'NONE' AND deleted_at IS NULL;

-- LOT: one record per lot per product per location.
CREATE UNIQUE INDEX ux_inventories_lot_position
    ON inventories (company_id, product_id, location_id, lot_number)
    WHERE tracking_type = 'LOT' AND deleted_at IS NULL;

-- SERIAL: a serial number is a globally unique physical unit (an IMEI, a VIN),
-- so the constraint is on the serial ALONE — the same serial cannot exist twice
-- anywhere, not merely once per company.
CREATE UNIQUE INDEX ux_inventories_serial
    ON inventories (serial_number)
    WHERE tracking_type = 'SERIAL' AND deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Lookup indexes
-- ---------------------------------------------------------------------------
--
-- Every query is company-scoped, so company_id leads each composite. These
-- serve the repository's find methods and the operational screens that ask
-- "what is in this warehouse / of this product / in this location".

CREATE INDEX idx_inventories_company_warehouse
    ON inventories (company_id, warehouse_id)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_inventories_company_product
    ON inventories (company_id, product_id)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_inventories_company_location
    ON inventories (company_id, location_id)
    WHERE deleted_at IS NULL;

-- The hot path: every stock record for a product in a location (one row for
-- NONE, many for LOT/SERIAL). Backs FindByProductLocation.
CREATE INDEX idx_inventories_company_product_location
    ON inventories (company_id, product_id, location_id)
    WHERE deleted_at IS NULL;

-- Serial and lot lookups. The serial index is on the bare column because a scan
-- resolves a serial with no other context; the lot lookup is company-scoped.
CREATE INDEX idx_inventories_serial_number
    ON inventories (serial_number)
    WHERE serial_number IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX idx_inventories_lot_number
    ON inventories (lot_number)
    WHERE lot_number IS NOT NULL AND deleted_at IS NULL;
