-- 20260808100000: create_inventory_ledger_entries_table (down)
--
-- The trigger is dropped with the table; the function is not, so it is removed
-- explicitly. Nothing references inventory_ledger_entries, so the table itself
-- drops cleanly.

DROP TABLE IF EXISTS inventory_ledger_entries;
DROP FUNCTION IF EXISTS inventory_ledger_entries_reject_mutation();
