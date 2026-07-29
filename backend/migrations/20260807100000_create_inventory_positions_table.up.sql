-- 20260807100000: create_inventory_positions_table (up)
--
-- The InventoryPosition model: the stock of one product, with one set of stock
-- attributes, in one storage location.
--
-- Four balances are stored — available, reserved, allocated, quarantined — and
-- on-hand is DERIVED as their sum. It runs after products because every position
-- references one.

CREATE TABLE inventory_positions (
    id             UUID PRIMARY KEY,

    company_id     UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    -- RESTRICT on all three: a warehouse, location or product that still carries
    -- stock must not be hard-deleted out from under its positions.
    warehouse_id   UUID NOT NULL REFERENCES warehouses (id)        ON DELETE RESTRICT,
    location_id    UUID NOT NULL REFERENCES storage_locations (id) ON DELETE RESTRICT,
    product_id     UUID NOT NULL REFERENCES products (id)          ON DELETE RESTRICT,

    -- The StockAttributes triple. NONE | LOT | SERIAL, with the lot or serial
    -- that individuates the position. CITEXT because a scanned lot or serial is
    -- the same identifier whatever its case.
    tracking_type  VARCHAR(16) NOT NULL,
    lot_number     CITEXT,
    serial_number  CITEXT,

    -- The four buckets. Every unit of stock sits in exactly one of them.
    --
    -- on_hand is DELIBERATELY ABSENT: it is available+reserved+allocated+
    -- quarantined, derived by the aggregate. Storing it would create a second
    -- source of truth that could disagree with its own parts.
    available      BIGINT NOT NULL DEFAULT 0,
    reserved       BIGINT NOT NULL DEFAULT 0,
    allocated      BIGINT NOT NULL DEFAULT 0,
    quarantined    BIGINT NOT NULL DEFAULT 0,

    version        BIGINT NOT NULL DEFAULT 1,

    created_by     UUID NOT NULL REFERENCES users (id),
    updated_by     UUID NOT NULL REFERENCES users (id),

    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    deleted_at     TIMESTAMPTZ,

    CONSTRAINT inventory_positions_tracking_check
        CHECK (tracking_type IN ('NONE', 'LOT', 'SERIAL')),

    -- Every bucket is non-negative. The aggregate is the primary guard; this is
    -- the backstop for anything that reaches the table another way.
    CONSTRAINT inventory_positions_buckets_check
        CHECK (available >= 0 AND reserved >= 0 AND allocated >= 0 AND quarantined >= 0),

    -- The StockAttributes rule, restated in SQL: the lot/serial presence must
    -- match the tracking type, and a serial position never exceeds one unit.
    CONSTRAINT inventory_positions_attributes_check
        CHECK (
            (tracking_type = 'NONE'   AND lot_number IS NULL     AND serial_number IS NULL) OR
            (tracking_type = 'LOT'    AND lot_number IS NOT NULL AND serial_number IS NULL) OR
            (tracking_type = 'SERIAL' AND serial_number IS NOT NULL AND lot_number IS NULL
                                      AND available + reserved + allocated + quarantined <= 1)
        )
);

-- ---------------------------------------------------------------------------
-- Uniqueness: one position per StockKey
-- ---------------------------------------------------------------------------
--
-- The key is (company, warehouse, location, product, attributes). Because NULLs
-- do not compare equal in a UNIQUE index, each tracking type gets its own PARTIAL
-- index over exactly the columns that are non-NULL for it.

-- NONE: one fungible pool per product per location.
CREATE UNIQUE INDEX ux_inventory_positions_none
    ON inventory_positions (company_id, warehouse_id, location_id, product_id)
    WHERE tracking_type = 'NONE' AND deleted_at IS NULL;

-- LOT: one position per lot per product per location.
CREATE UNIQUE INDEX ux_inventory_positions_lot
    ON inventory_positions (company_id, warehouse_id, location_id, product_id, lot_number)
    WHERE tracking_type = 'LOT' AND deleted_at IS NULL;

-- SERIAL: a serial is a globally unique physical unit, so the constraint is on
-- the serial alone — the same unit cannot exist in two places at once.
CREATE UNIQUE INDEX ux_inventory_positions_serial
    ON inventory_positions (serial_number)
    WHERE tracking_type = 'SERIAL' AND deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Lookup indexes
-- ---------------------------------------------------------------------------
--
-- Every query is company-scoped, so company_id leads each composite.

CREATE INDEX idx_inventory_positions_company_warehouse
    ON inventory_positions (company_id, warehouse_id)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_inventory_positions_company_product
    ON inventory_positions (company_id, product_id)
    WHERE deleted_at IS NULL;

-- The hot path: resolving a position by its key, and listing what a location
-- holds.
CREATE INDEX idx_inventory_positions_company_location_product
    ON inventory_positions (company_id, location_id, product_id)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_inventory_positions_lot_number
    ON inventory_positions (lot_number)
    WHERE lot_number IS NOT NULL AND deleted_at IS NULL;
