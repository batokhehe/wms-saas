-- 20260722100000: create_users_table (down)
--
-- Every up migration must be reversible. A migration that cannot be rolled back
-- cannot be deployed safely.
--
-- Indexes are dropped implicitly with the table; they are named here only for
-- clarity about what this migration owns.

DROP TABLE IF EXISTS users;
