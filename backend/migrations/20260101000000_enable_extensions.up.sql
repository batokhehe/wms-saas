-- 20260101000000: enable_extensions (up)
--
-- Infrastructure only: no business tables are created here.
--
-- pgcrypto supplies gen_random_uuid(). Primary keys are UUIDs rather than
-- bigserial because a multi-tenant WMS syncs records from offline mobile
-- clients, and a client that cannot mint its own identifiers has to round-trip
-- to the server before it can reference anything it just created.
--
-- citext gives case-insensitive columns for values like SKU codes and emails,
-- where "ABC-1" and "abc-1" must not be two different rows.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "citext";
