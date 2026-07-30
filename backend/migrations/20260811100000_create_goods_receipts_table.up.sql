CREATE TABLE goods_receipts (
    id UUID PRIMARY KEY,
    number VARCHAR(32) NOT NULL,
    company_id UUID NOT NULL REFERENCES companies(id),
    warehouse_id UUID NOT NULL REFERENCES warehouses(id),
    supplier_id UUID REFERENCES suppliers(id),
    reference_type VARCHAR(32) NOT NULL,
    reference_id UUID,
    receipt_date TIMESTAMP WITH TIME ZONE NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'DRAFT',
    remarks TEXT DEFAULT '',
    created_by UUID NOT NULL,
    received_by UUID,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE goods_receipt_lines (
    id UUID PRIMARY KEY,
    goods_receipt_id UUID NOT NULL REFERENCES goods_receipts(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id),
    uom_id UUID NOT NULL REFERENCES uoms(id),
    quantity NUMERIC(19, 6) NOT NULL,
    batch_number VARCHAR(64),
    lot_number VARCHAR(64),
    serial_numbers TEXT[],
    expiry_date TIMESTAMP WITH TIME ZONE,
    remarks TEXT DEFAULT ''
);

CREATE UNIQUE INDEX ux_goods_receipts_number ON goods_receipts(number);
CREATE INDEX ix_goods_receipts_company ON goods_receipts(company_id);
