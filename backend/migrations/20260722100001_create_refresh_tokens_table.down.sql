-- 20260722100001: create_refresh_tokens_table (down)
--
-- Dropped before users in rollback order, because refresh_tokens holds the
-- foreign key. Rolling back in the wrong order would fail on the constraint.

DROP TABLE IF EXISTS refresh_tokens;
