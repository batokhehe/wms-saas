CREATE TABLE putaways (
    id UUID PRIMARY KEY,
    number VARCHAR(32) NOT NULL,
    company_id UUID NOT NULL REFERENCES companies(id),
    warehouse_id UUID NOT NULL REFERENCES warehouses(id),
    goods_receipt_id UUID REFERENCES goods_receipts(id),
    quality_inspection_id UUID REFERENCES quality_inspections(id),
    status VARCHAR(16) NOT NULL DEFAULT 'DRAFT',
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE putaway_lines (
    id UUID PRIMARY KEY,
    putaway_id UUID NOT NULL REFERENCES putaways(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id),
    from_location_id UUID NOT NULL REFERENCES storage_locations(id),
    to_location_id UUID NOT NULL REFERENCES storage_locations(id),
    quantity NUMERIC(19, 6) NOT NULL
);

CREATE UNIQUE INDEX ux_putaways_number ON putaways(number);
CREATE INDEX ix_putaways_company ON putaways(company_id);
