-- 20260808100000: create_inventory_ledger_entries_table (up)
--
-- The inventory ledger: one immutable row per stock transition.
--
-- It NEVER owns stock. Every quantity here is a SNAPSHOT of what
-- inventory_positions reported at the moment of the movement, kept so history can
-- be read without replaying it. No process reads a balance from this table to
-- decide whether stock may move.
--
-- It runs after inventory_positions because every entry references one.

CREATE TABLE inventory_ledger_entries (
    id                  UUID PRIMARY KEY,

    company_id          UUID NOT NULL REFERENCES companies (id) ON DELETE CASCADE,

    -- The position whose state changed. RESTRICT, not CASCADE: deleting a
    -- position must never erase the history of what happened to it.
    position_id         UUID NOT NULL REFERENCES inventory_positions (id) ON DELETE RESTRICT,

    -- Denormalised from the position so a ledger query can filter by product or
    -- warehouse without joining a row that may since have moved on. A ledger
    -- records what was true THEN; resolving it through today's position would
    -- quietly rewrite the past.
    product_id          UUID NOT NULL REFERENCES products (id)          ON DELETE RESTRICT,
    warehouse_id        UUID NOT NULL REFERENCES warehouses (id)        ON DELETE RESTRICT,
    location_id         UUID NOT NULL REFERENCES storage_locations (id) ON DELETE RESTRICT,

    lot_number          CITEXT,
    serial_number       CITEXT,

    -- The party the stock belongs to when it is not the operating company
    -- (consignment, 3PL). NULL means the company owns it. No foreign key: the
    -- owner may be a supplier, a customer or an external party, and no single
    -- table can be named.
    owner_id            UUID,

    movement_type       VARCHAR(24) NOT NULL,

    -- Provenance: what document caused the movement, and why.
    reference_type      VARCHAR(64),
    reference_id        UUID,
    document_number     VARCHAR(64),
    reason              TEXT NOT NULL DEFAULT '',

    actor_id            UUID NOT NULL REFERENCES users (id),

    -- The four balances before and after. Stored rather than derived because the
    -- ledger's job is to say what the position looked like at that instant, which
    -- no later computation can recover.
    before_available    BIGINT NOT NULL,
    before_reserved     BIGINT NOT NULL,
    before_allocated    BIGINT NOT NULL,
    before_quarantined  BIGINT NOT NULL,

    after_available     BIGINT NOT NULL,
    after_reserved      BIGINT NOT NULL,
    after_allocated     BIGINT NOT NULL,
    after_quarantined   BIGINT NOT NULL,

    -- The signed change. Redundant with before/after by design: persisting it
    -- lets "what moved this period" be a SUM instead of loading every row to
    -- subtract four numbers in application code. A CHECK keeps it honest.
    delta_available     BIGINT NOT NULL,
    delta_reserved      BIGINT NOT NULL,
    delta_allocated     BIGINT NOT NULL,
    delta_quarantined   BIGINT NOT NULL,
    delta_on_hand       BIGINT NOT NULL,

    -- BUSINESS time: when the stock actually moved, which a backdated correction
    -- sets to the past. created_at is when the row was written; the two differ.
    occurred_at         TIMESTAMPTZ NOT NULL,

    -- Inherited from the shared BaseEntity so the generic repository can page and
    -- scope these rows. Both are inert: version never advances (the row is never
    -- updated) and deleted_at is never set (an entry is never removed).
    version             BIGINT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ,

    CONSTRAINT inventory_ledger_movement_type_check
        CHECK (movement_type IN (
            'INITIAL_BALANCE', 'INBOUND', 'OUTBOUND', 'TRANSFER', 'RESERVATION',
            'ALLOCATION', 'ADJUSTMENT', 'QUARANTINE', 'CYCLE_COUNT'
        )),

    -- Balances are snapshots of a position, and a position's buckets are never
    -- negative.
    CONSTRAINT inventory_ledger_balances_check
        CHECK (
            before_available >= 0 AND before_reserved >= 0 AND
            before_allocated >= 0 AND before_quarantined >= 0 AND
            after_available  >= 0 AND after_reserved  >= 0 AND
            after_allocated  >= 0 AND after_quarantined  >= 0
        ),

    -- The stored delta must equal after minus before. This is what stops the
    -- denormalised columns from ever contradicting the snapshots beside them.
    CONSTRAINT inventory_ledger_delta_check
        CHECK (
            delta_available   = after_available   - before_available  AND
            delta_reserved    = after_reserved    - before_reserved   AND
            delta_allocated   = after_allocated   - before_allocated  AND
            delta_quarantined = after_quarantined - before_quarantined AND
            delta_on_hand     = (after_available + after_reserved + after_allocated + after_quarantined)
                              - (before_available + before_reserved + before_allocated + before_quarantined)
        ),

    -- A unit is either a batch or an individual, never both.
    CONSTRAINT inventory_ledger_tracking_check
        CHECK (lot_number IS NULL OR serial_number IS NULL),

    -- A reference id nobody can resolve is worse than none.
    CONSTRAINT inventory_ledger_reference_check
        CHECK (reference_id IS NULL OR reference_type IS NOT NULL)
);

-- ---------------------------------------------------------------------------
-- Indexes
-- ---------------------------------------------------------------------------
--
-- Every query is company-scoped, so company_id leads each composite. Each index
-- below answers one real question rather than covering a column speculatively.

-- "What happened to this position?" — the position history screen. occurred_at
-- descends because history is always read newest-first.
CREATE INDEX ix_inventory_ledger_company_position_occurred
    ON inventory_ledger_entries (company_id, position_id, occurred_at DESC);

-- "What happened to this product?" — across every location.
CREATE INDEX ix_inventory_ledger_company_product_occurred
    ON inventory_ledger_entries (company_id, product_id, occurred_at DESC);

-- "What happened in this warehouse?" — the site-level movement report.
CREATE INDEX ix_inventory_ledger_company_warehouse_occurred
    ON inventory_ledger_entries (company_id, warehouse_id, occurred_at DESC);

-- The plain date-range scan, when no other dimension is filtered.
CREATE INDEX ix_inventory_ledger_company_occurred
    ON inventory_ledger_entries (company_id, occurred_at DESC);

-- "Show me every adjustment last month" — movement type with the period.
CREATE INDEX ix_inventory_ledger_company_movement_occurred
    ON inventory_ledger_entries (company_id, movement_type, occurred_at DESC);

-- "What did this document actually move?" — partial, because most entries have
-- no reference and indexing their NULLs would waste the tree.
CREATE INDEX ix_inventory_ledger_reference
    ON inventory_ledger_entries (company_id, reference_type, reference_id)
    WHERE reference_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Immutability
-- ---------------------------------------------------------------------------
--
-- The application cannot rewrite the ledger — the aggregate has no setters and
-- the repository exposes no Update or Delete. This trigger makes that guarantee
-- independent of the application: a stray query, a migration script or a psql
-- session is refused too.
--
-- It also blocks GORM's soft delete, which issues an UPDATE to set deleted_at.
-- That is intended: an entry is never removed by any means.

CREATE OR REPLACE FUNCTION inventory_ledger_entries_reject_mutation()
RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION
        'inventory_ledger_entries is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_inventory_ledger_entries_immutable
    BEFORE UPDATE OR DELETE ON inventory_ledger_entries
    FOR EACH ROW
    EXECUTE FUNCTION inventory_ledger_entries_reject_mutation();
