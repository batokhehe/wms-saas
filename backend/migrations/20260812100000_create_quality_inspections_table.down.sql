-- Child first: quality_inspection_lines has FKs to quality_inspections and
-- goods_receipt_lines.
DROP INDEX IF EXISTS ix_quality_inspections_company;
DROP INDEX IF EXISTS ux_quality_inspections_number;
DROP TABLE IF EXISTS quality_inspection_lines;
DROP TABLE IF EXISTS quality_inspections;
