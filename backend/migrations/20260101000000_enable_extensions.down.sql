-- 20260101000000: enable_extensions (down)
--
-- Extensions are dropped in reverse order of creation. DROP EXTENSION fails
-- rather than cascading if any column still depends on the type, which is the
-- safe default: a rollback must not silently delete columns.

DROP EXTENSION IF EXISTS "citext";
DROP EXTENSION IF EXISTS "pgcrypto";
