-- Child first: putaway_lines has a FK to putaways.
DROP INDEX IF EXISTS ix_putaways_company;
DROP INDEX IF EXISTS ux_putaways_number;
DROP TABLE IF EXISTS putaway_lines;
DROP TABLE IF EXISTS putaways;
