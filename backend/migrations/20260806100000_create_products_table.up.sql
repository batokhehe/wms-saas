CREATE TABLE products (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL REFERENCES companies(id),
    category_id UUID NOT NULL REFERENCES categories(id),
    brand_id UUID NOT NULL REFERENCES brands(id),
    base_uom_id UUID NOT NULL REFERENCES uoms(id),
    sku CITEXT NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    tracking_config JSONB NOT NULL,
    inventory_policy JSONB NOT NULL,
    dimensions JSONB NOT NULL,
    weight JSONB NOT NULL,
    volume JSONB NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE',
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE product_barcodes (
    id UUID PRIMARY KEY,
    product_id UUID NOT NULL REFERENCES products(id),
    code CITEXT NOT NULL,
    type VARCHAR(50) NOT NULL
);

CREATE TABLE product_alternate_uoms (
    id UUID PRIMARY KEY,
    product_id UUID NOT NULL REFERENCES products(id),
    uom_id UUID NOT NULL REFERENCES uoms(id),
    factor NUMERIC(19, 6) NOT NULL
);

CREATE UNIQUE INDEX ux_products_company_sku ON products(company_id, sku) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX ux_product_barcodes_code ON product_barcodes(code);
CREATE INDEX ix_products_company_status ON products(company_id, status) WHERE deleted_at IS NULL;
