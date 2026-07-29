-- 20260807100000: create_inventory_positions_table (down)
--
-- One table, no dependants: nothing references inventory_positions, so it drops
-- cleanly. Its indexes and CHECK constraints go with it.

DROP TABLE IF EXISTS inventory_positions;
