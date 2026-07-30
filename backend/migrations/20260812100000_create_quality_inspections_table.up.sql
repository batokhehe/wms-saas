CREATE TABLE quality_inspections (
    id UUID PRIMARY KEY,
    number VARCHAR(32) NOT NULL,
    company_id UUID NOT NULL REFERENCES companies(id),
    goods_receipt_id UUID NOT NULL REFERENCES goods_receipts(id),
    warehouse_id UUID NOT NULL REFERENCES warehouses(id),
    inspector_id UUID NOT NULL,
    inspection_date TIMESTAMP WITH TIME ZONE NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'DRAFT',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE quality_inspection_lines (
    id UUID PRIMARY KEY,
    quality_inspection_id UUID NOT NULL REFERENCES quality_inspections(id) ON DELETE CASCADE,
    goods_receipt_line_id UUID NOT NULL REFERENCES goods_receipt_lines(id),
    product_id UUID NOT NULL REFERENCES products(id),
    expected_qty NUMERIC(19, 6) NOT NULL,
    accepted_qty NUMERIC(19, 6) NOT NULL,
    rejected_qty NUMERIC(19, 6) NOT NULL,
    reason VARCHAR(64),
    remarks TEXT
);

CREATE UNIQUE INDEX ux_quality_inspections_number ON quality_inspections(number);
CREATE INDEX ix_quality_inspections_company ON quality_inspections(company_id);
