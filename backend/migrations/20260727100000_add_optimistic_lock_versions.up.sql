-- Add the BaseEntity optimistic-lock token to every persisted BaseEntity.
-- Nullable/backfill/default/not-null keeps deployment safe for populated tables.
ALTER TABLE users ADD COLUMN version BIGINT;
ALTER TABLE refresh_tokens ADD COLUMN version BIGINT;
ALTER TABLE companies ADD COLUMN version BIGINT;
ALTER TABLE memberships ADD COLUMN version BIGINT;
ALTER TABLE permissions ADD COLUMN version BIGINT;
ALTER TABLE roles ADD COLUMN version BIGINT;
ALTER TABLE role_permissions ADD COLUMN version BIGINT;
ALTER TABLE warehouses ADD COLUMN version BIGINT;
ALTER TABLE storage_locations ADD COLUMN version BIGINT;

UPDATE users SET version = 1 WHERE version IS NULL;
UPDATE refresh_tokens SET version = 1 WHERE version IS NULL;
UPDATE companies SET version = 1 WHERE version IS NULL;
UPDATE memberships SET version = 1 WHERE version IS NULL;
UPDATE permissions SET version = 1 WHERE version IS NULL;
UPDATE roles SET version = 1 WHERE version IS NULL;
UPDATE role_permissions SET version = 1 WHERE version IS NULL;
UPDATE warehouses SET version = 1 WHERE version IS NULL;
UPDATE storage_locations SET version = 1 WHERE version IS NULL;

ALTER TABLE users ALTER COLUMN version SET DEFAULT 1, ALTER COLUMN version SET NOT NULL;
ALTER TABLE refresh_tokens ALTER COLUMN version SET DEFAULT 1, ALTER COLUMN version SET NOT NULL;
ALTER TABLE companies ALTER COLUMN version SET DEFAULT 1, ALTER COLUMN version SET NOT NULL;
ALTER TABLE memberships ALTER COLUMN version SET DEFAULT 1, ALTER COLUMN version SET NOT NULL;
ALTER TABLE permissions ALTER COLUMN version SET DEFAULT 1, ALTER COLUMN version SET NOT NULL;
ALTER TABLE roles ALTER COLUMN version SET DEFAULT 1, ALTER COLUMN version SET NOT NULL;
ALTER TABLE role_permissions ALTER COLUMN version SET DEFAULT 1, ALTER COLUMN version SET NOT NULL;
ALTER TABLE warehouses ALTER COLUMN version SET DEFAULT 1, ALTER COLUMN version SET NOT NULL;
ALTER TABLE storage_locations ALTER COLUMN version SET DEFAULT 1, ALTER COLUMN version SET NOT NULL;
