-- Align the UOM table with shared/entity.BaseEntity.
--
-- The nullable-add/backfill/default/not-null sequence is safe for deployments
-- where UOM rows may already exist.
ALTER TABLE uoms ADD COLUMN version BIGINT;

UPDATE uoms SET version = 1 WHERE version IS NULL;

ALTER TABLE uoms
    ALTER COLUMN version SET DEFAULT 1,
    ALTER COLUMN version SET NOT NULL;
