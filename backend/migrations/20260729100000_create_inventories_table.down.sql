-- 20260729100000: create_inventories_table (down)
--
-- Inventory owns no child tables, so the single table drops cleanly. Its foreign
-- keys are OUTBOUND (into companies, warehouses, storage_locations, products,
-- users); nothing yet references inventories, so no drop ordering is required.

DROP TABLE IF EXISTS inventories;
