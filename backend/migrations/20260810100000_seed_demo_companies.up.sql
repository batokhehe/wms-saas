-- 20260810100000: seed_demo_companies (up)
--
-- Production-quality demo data for TWO fully isolated tenants, so a Flutter
-- client can be built and exercised against realistic content.
--
--   Company A — PT Alpha Manufacturing (ALPHA): makes goods, so its catalogue is
--               raw material in / finished goods out.
--   Company B — PT Beta Distribution  (BETA):  resells goods, so its catalogue is
--               finished goods and spare parts.
--
-- # Isolation
--
-- Every row below except the UOM table carries a company_id, and the two
-- companies share NO master data: separate warehouses, locations, categories,
-- brands, products, suppliers, customers, stock and ledger history. UOM is the
-- single deliberate exception — a kilogram is a kilogram — and the uoms table has
-- no company_id at all, which is why it is seeded once here rather than twice.
--
-- # Role mapping
--
-- The requested personas are Admin / Supervisor / Operator, but memberships.role
-- is constrained to OWNER | ADMIN | STAFF. The personas map onto those three
-- rather than inventing values the CHECK would reject:
--
--   Admin      -> OWNER  (full control of the tenant)
--   Supervisor -> ADMIN  (runs operations, cannot delete or restructure RBAC)
--   Operator   -> STAFF  (floor work: move, reserve, count)
--
-- Every user's password is the bcrypt hash of "Password123!".
--
-- # Idempotency
--
-- Identifiers are fixed literals rather than gen_random_uuid(), so a Flutter
-- developer can hard-code them while building screens and they survive a reset.
-- Every statement is ON CONFLICT DO NOTHING so re-running is harmless.

-- ---------------------------------------------------------------------------
-- Global UOM (shared by design — the ONLY shared master data)
-- ---------------------------------------------------------------------------

INSERT INTO uoms (id, code, name, status, version, created_at, updated_at) VALUES
    ('0a000000-0000-4000-a000-000000000001', 'PCS', 'Pieces',     'ACTIVE', 1, NOW(), NOW()),
    ('0a000000-0000-4000-a000-000000000002', 'BOX', 'Box',        'ACTIVE', 1, NOW(), NOW()),
    ('0a000000-0000-4000-a000-000000000003', 'KG',  'Kilogram',   'ACTIVE', 1, NOW(), NOW()),
    ('0a000000-0000-4000-a000-000000000004', 'LTR', 'Liter',      'ACTIVE', 1, NOW(), NOW()),
    ('0a000000-0000-4000-a000-000000000005', 'ROL', 'Roll',       'ACTIVE', 1, NOW(), NOW()),
    ('0a000000-0000-4000-a000-000000000006', 'SAK', 'Sak (Sack)', 'ACTIVE', 1, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Companies
-- ---------------------------------------------------------------------------

INSERT INTO companies (id, code, name, status, version, created_at, updated_at) VALUES
    ('a0000000-0000-4000-a000-000000000001', 'ALPHA', 'PT Alpha Manufacturing', 'ACTIVE', 1, NOW(), NOW()),
    ('b0000000-0000-4000-b000-000000000001', 'BETA',  'PT Beta Distribution',   'ACTIVE', 1, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Users — three personas per company
-- ---------------------------------------------------------------------------

INSERT INTO users (id, email, password_hash, full_name, status, version, created_at, updated_at) VALUES
    ('a1000000-0000-4000-a000-000000000001', 'admin@alpha.co.id',      crypt('Password123!', gen_salt('bf', 12)), 'Budi Santoso',      'ACTIVE', 1, NOW(), NOW()),
    ('a1000000-0000-4000-a000-000000000002', 'supervisor@alpha.co.id', crypt('Password123!', gen_salt('bf', 12)), 'Siti Rahayu',       'ACTIVE', 1, NOW(), NOW()),
    ('a1000000-0000-4000-a000-000000000003', 'operator@alpha.co.id',   crypt('Password123!', gen_salt('bf', 12)), 'Agus Prasetyo',     'ACTIVE', 1, NOW(), NOW()),
    ('b1000000-0000-4000-b000-000000000001', 'admin@beta.co.id',       crypt('Password123!', gen_salt('bf', 12)), 'Dewi Lestari',      'ACTIVE', 1, NOW(), NOW()),
    ('b1000000-0000-4000-b000-000000000002', 'supervisor@beta.co.id',  crypt('Password123!', gen_salt('bf', 12)), 'Rudi Hartono',      'ACTIVE', 1, NOW(), NOW()),
    ('b1000000-0000-4000-b000-000000000003', 'operator@beta.co.id',    crypt('Password123!', gen_salt('bf', 12)), 'Putri Anggraini',   'ACTIVE', 1, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Roles — the three system roles per company, as the RBAC provisioner makes them
-- ---------------------------------------------------------------------------

INSERT INTO roles (id, company_id, name, description, is_system, version, created_at, updated_at) VALUES
    ('a2000000-0000-4000-a000-000000000001', 'a0000000-0000-4000-a000-000000000001', 'OWNER', 'Full access to the company, including deletion and role management.', TRUE, 1, NOW(), NOW()),
    ('a2000000-0000-4000-a000-000000000002', 'a0000000-0000-4000-a000-000000000001', 'ADMIN', 'Manages members and company settings.',                                TRUE, 1, NOW(), NOW()),
    ('a2000000-0000-4000-a000-000000000003', 'a0000000-0000-4000-a000-000000000001', 'STAFF', 'Day-to-day operations with read access to administration.',            TRUE, 1, NOW(), NOW()),
    ('b2000000-0000-4000-b000-000000000001', 'b0000000-0000-4000-b000-000000000001', 'OWNER', 'Full access to the company, including deletion and role management.', TRUE, 1, NOW(), NOW()),
    ('b2000000-0000-4000-b000-000000000002', 'b0000000-0000-4000-b000-000000000001', 'ADMIN', 'Manages members and company settings.',                                TRUE, 1, NOW(), NOW()),
    ('b2000000-0000-4000-b000-000000000003', 'b0000000-0000-4000-b000-000000000001', 'STAFF', 'Day-to-day operations with read access to administration.',            TRUE, 1, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- Grants. Derived from the catalogue by CODE rather than listed by id, so this
-- seed cannot drift from entity.DefaultPermissions as new modules add codes.

-- OWNER: everything the software can enforce.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r CROSS JOIN permissions p
WHERE r.name = 'OWNER' AND r.company_id IN ('a0000000-0000-4000-a000-000000000001','b0000000-0000-4000-b000-000000000001')
  AND NOT EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role_id = r.id AND rp.permission_id = p.id);

-- ADMIN: operational. No deletes, no RBAC restructuring, no manual stock adjustment.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r CROSS JOIN permissions p
WHERE r.name = 'ADMIN' AND r.company_id IN ('a0000000-0000-4000-a000-000000000001','b0000000-0000-4000-b000-000000000001')
  AND p.code NOT LIKE '%.delete'
  AND p.code NOT IN ('company.delete','inventory.adjust','product.discontinue',
                     'role.create','role.update','role.delete','role.assign_permissions')
  AND NOT EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role_id = r.id AND rp.permission_id = p.id);

-- STAFF: read the master data, act on stock.
INSERT INTO role_permissions (id, role_id, permission_id, created_at, updated_at)
SELECT gen_random_uuid(), r.id, p.id, NOW(), NOW()
FROM roles r CROSS JOIN permissions p
WHERE r.name = 'STAFF' AND r.company_id IN ('a0000000-0000-4000-a000-000000000001','b0000000-0000-4000-b000-000000000001')
  AND (p.code LIKE '%.read'
       OR p.code IN ('inventory.update','inventory.reserve','inventory.transfer','inventory.cyclecount'))
  AND NOT EXISTS (SELECT 1 FROM role_permissions rp WHERE rp.role_id = r.id AND rp.permission_id = p.id);

-- ---------------------------------------------------------------------------
-- Memberships
-- ---------------------------------------------------------------------------

INSERT INTO memberships (id, company_id, user_id, role, status, version, created_at, updated_at) VALUES
    ('a3000000-0000-4000-a000-000000000001', 'a0000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', 'OWNER', 'ACTIVE', 1, NOW(), NOW()),
    ('a3000000-0000-4000-a000-000000000002', 'a0000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000002', 'ADMIN', 'ACTIVE', 1, NOW(), NOW()),
    ('a3000000-0000-4000-a000-000000000003', 'a0000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000003', 'STAFF', 'ACTIVE', 1, NOW(), NOW()),
    ('b3000000-0000-4000-b000-000000000001', 'b0000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', 'OWNER', 'ACTIVE', 1, NOW(), NOW()),
    ('b3000000-0000-4000-b000-000000000002', 'b0000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000002', 'ADMIN', 'ACTIVE', 1, NOW(), NOW()),
    ('b3000000-0000-4000-b000-000000000003', 'b0000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000003', 'STAFF', 'ACTIVE', 1, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Warehouses — three per company
-- ---------------------------------------------------------------------------

INSERT INTO warehouses (id, company_id, code, name, description, type, status, address, contact_name, contact_phone, version, created_by, updated_by, created_at, updated_at) VALUES
    ('a4000000-0000-4000-a000-000000000001', 'a0000000-0000-4000-a000-000000000001', 'ALP-WH-01', 'Gudang Utama Cikarang',      'Main warehouse',        'MAIN',   'ACTIVE', 'Jl. Jababeka Raya Blok C No. 12, Cikarang, Bekasi', 'Budi Santoso',  '+62-21-8934-1001', 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('a4000000-0000-4000-a000-000000000002', 'a0000000-0000-4000-a000-000000000001', 'ALP-WH-02', 'Gudang Barang Jadi',         'Finished goods',        'BRANCH', 'ACTIVE', 'Jl. Jababeka Raya Blok C No. 14, Cikarang, Bekasi', 'Siti Rahayu',   '+62-21-8934-1002', 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('a4000000-0000-4000-a000-000000000003', 'a0000000-0000-4000-a000-000000000001', 'ALP-WH-03', 'Gudang Bahan Baku',          'Raw material',          'BRANCH', 'ACTIVE', 'Jl. Jababeka Raya Blok C No. 16, Cikarang, Bekasi', 'Agus Prasetyo', '+62-21-8934-1003', 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('b4000000-0000-4000-b000-000000000001', 'b0000000-0000-4000-b000-000000000001', 'BET-WH-01', 'Gudang Utama Tangerang',     'Main warehouse',        'MAIN',   'ACTIVE', 'Jl. Raya Serpong KM 8, Tangerang Selatan, Banten',  'Dewi Lestari',  '+62-21-5312-2001', 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW()),
    ('b4000000-0000-4000-b000-000000000002', 'b0000000-0000-4000-b000-000000000001', 'BET-WH-02', 'Gudang Barang Jadi Serpong', 'Finished goods',        'BRANCH', 'ACTIVE', 'Jl. Raya Serpong KM 9, Tangerang Selatan, Banten',  'Rudi Hartono',  '+62-21-5312-2002', 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW()),
    ('b4000000-0000-4000-b000-000000000003', 'b0000000-0000-4000-b000-000000000001', 'BET-WH-03', 'Gudang Transit Bandara',     'Raw material/transit',  'TRANSIT','ACTIVE', 'Kawasan Pergudangan Soekarno-Hatta, Tangerang',     'Putri Anggraini','+62-21-5312-2003', 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Storage locations — six in each company's MAIN warehouse
-- ---------------------------------------------------------------------------

INSERT INTO storage_locations (id, company_id, warehouse_id, code, zone, aisle, rack, level, bin, barcode, status, picking_priority, allow_mixed_sku, allow_overflow, max_weight, max_volume, max_pallet, version, created_by, updated_by, created_at, updated_at) VALUES
    ('a5000000-0000-4000-a000-000000000001', 'a0000000-0000-4000-a000-000000000001', 'a4000000-0000-4000-a000-000000000001', 'RACK-A', 'STORAGE',   'A', '01', '1', '01', 'ALP-LOC-RACKA', 'ACTIVE', 10,  FALSE, FALSE, 2000.000, 40.000, 12, 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('a5000000-0000-4000-a000-000000000002', 'a0000000-0000-4000-a000-000000000001', 'a4000000-0000-4000-a000-000000000001', 'RACK-B', 'STORAGE',   'B', '01', '1', '01', 'ALP-LOC-RACKB', 'ACTIVE', 20,  FALSE, FALSE, 2000.000, 40.000, 12, 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('a5000000-0000-4000-a000-000000000003', 'a0000000-0000-4000-a000-000000000001', 'a4000000-0000-4000-a000-000000000001', 'RACK-C', 'STORAGE',   'C', '01', '1', '01', 'ALP-LOC-RACKC', 'ACTIVE', 30,  TRUE,  FALSE, 2000.000, 40.000, 12, 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('a5000000-0000-4000-a000-000000000004', 'a0000000-0000-4000-a000-000000000001', 'a4000000-0000-4000-a000-000000000001', 'FLOOR',  'FLOOR',     '',  '',   '',  '',   'ALP-LOC-FLOOR', 'ACTIVE', 50,  TRUE,  TRUE,  8000.000, 200.000, 40, 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('a5000000-0000-4000-a000-000000000005', 'a0000000-0000-4000-a000-000000000001', 'a4000000-0000-4000-a000-000000000001', 'RECV',   'RECEIVING', '',  '',   '',  '',   'ALP-LOC-RECV',  'ACTIVE', 90,  TRUE,  TRUE,  5000.000, 120.000, 24, 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('a5000000-0000-4000-a000-000000000006', 'a0000000-0000-4000-a000-000000000001', 'a4000000-0000-4000-a000-000000000001', 'SHIP',   'SHIPPING',  '',  '',   '',  '',   'ALP-LOC-SHIP',  'ACTIVE', 95,  TRUE,  TRUE,  5000.000, 120.000, 24, 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),

    ('b5000000-0000-4000-b000-000000000001', 'b0000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'RACK-A', 'STORAGE',   'A', '01', '1', '01', 'BET-LOC-RACKA', 'ACTIVE', 10,  FALSE, FALSE, 1500.000, 30.000, 10, 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW()),
    ('b5000000-0000-4000-b000-000000000002', 'b0000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'RACK-B', 'STORAGE',   'B', '01', '1', '01', 'BET-LOC-RACKB', 'ACTIVE', 20,  FALSE, FALSE, 1500.000, 30.000, 10, 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW()),
    ('b5000000-0000-4000-b000-000000000003', 'b0000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'RACK-C', 'STORAGE',   'C', '01', '1', '01', 'BET-LOC-RACKC', 'ACTIVE', 30,  TRUE,  FALSE, 1500.000, 30.000, 10, 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW()),
    ('b5000000-0000-4000-b000-000000000004', 'b0000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'FLOOR',  'FLOOR',     '',  '',   '',  '',   'BET-LOC-FLOOR', 'ACTIVE', 50,  TRUE,  TRUE,  6000.000, 150.000, 30, 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW()),
    ('b5000000-0000-4000-b000-000000000005', 'b0000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'RECV',   'RECEIVING', '',  '',   '',  '',   'BET-LOC-RECV',  'ACTIVE', 90,  TRUE,  TRUE,  4000.000, 100.000, 20, 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW()),
    ('b5000000-0000-4000-b000-000000000006', 'b0000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'SHIP',   'SHIPPING',  '',  '',   '',  '',   'BET-LOC-SHIP',  'ACTIVE', 95,  TRUE,  TRUE,  4000.000, 100.000, 20, 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Categories and brands — per company, never shared
-- ---------------------------------------------------------------------------

INSERT INTO categories (id, company_id, code, name, description, status, version, created_at, updated_at) VALUES
    ('a6000000-0000-4000-a000-000000000001', 'a0000000-0000-4000-a000-000000000001', 'FG',  'Barang Jadi',    'Finished goods', 'ACTIVE', 1, NOW(), NOW()),
    ('a6000000-0000-4000-a000-000000000002', 'a0000000-0000-4000-a000-000000000001', 'RM',  'Bahan Baku',     'Raw materials',  'ACTIVE', 1, NOW(), NOW()),
    ('a6000000-0000-4000-a000-000000000003', 'a0000000-0000-4000-a000-000000000001', 'PKG', 'Kemasan',        'Packaging',      'ACTIVE', 1, NOW(), NOW()),
    ('a6000000-0000-4000-a000-000000000004', 'a0000000-0000-4000-a000-000000000001', 'SP',  'Suku Cadang',    'Spare parts',    'ACTIVE', 1, NOW(), NOW()),
    ('b6000000-0000-4000-b000-000000000001', 'b0000000-0000-4000-b000-000000000001', 'FG',  'Barang Jadi',    'Finished goods', 'ACTIVE', 1, NOW(), NOW()),
    ('b6000000-0000-4000-b000-000000000002', 'b0000000-0000-4000-b000-000000000001', 'RM',  'Bahan Baku',     'Raw materials',  'ACTIVE', 1, NOW(), NOW()),
    ('b6000000-0000-4000-b000-000000000003', 'b0000000-0000-4000-b000-000000000001', 'PKG', 'Kemasan',        'Packaging',      'ACTIVE', 1, NOW(), NOW()),
    ('b6000000-0000-4000-b000-000000000004', 'b0000000-0000-4000-b000-000000000001', 'SP',  'Suku Cadang',    'Spare parts',    'ACTIVE', 1, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

INSERT INTO brands (id, company_id, code, name, description, status, version, created_at, updated_at) VALUES
    ('a7000000-0000-4000-a000-000000000001', 'a0000000-0000-4000-a000-000000000001', 'ALPHA',   'Alpha',        'House brand',        'ACTIVE', 1, NOW(), NOW()),
    ('a7000000-0000-4000-a000-000000000002', 'a0000000-0000-4000-a000-000000000001', 'NUSANTARA','Nusantara',   'Local supplier brand','ACTIVE', 1, NOW(), NOW()),
    ('b7000000-0000-4000-b000-000000000001', 'b0000000-0000-4000-b000-000000000001', 'BETA',    'Beta',         'House brand',        'ACTIVE', 1, NOW(), NOW()),
    ('b7000000-0000-4000-b000-000000000002', 'b0000000-0000-4000-b000-000000000001', 'SENTOSA', 'Sentosa',      'Distributed brand',  'ACTIVE', 1, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Products — four per company, one per category
-- ---------------------------------------------------------------------------
--
-- The JSONB columns use the Go value objects' exported field names verbatim,
-- because those types carry no json tags: a key mismatch would reconstitute as a
-- zero value and the seed would look valid while meaning nothing.

INSERT INTO products (id, company_id, category_id, brand_id, base_uom_id, sku, name, description, tracking_config, inventory_policy, dimensions, weight, volume, status, version, created_at, updated_at) VALUES
    ('a8000000-0000-4000-a000-000000000001', 'a0000000-0000-4000-a000-000000000001', 'a6000000-0000-4000-a000-000000000001', 'a7000000-0000-4000-a000-000000000001', '0a000000-0000-4000-a000-000000000001', 'ALP-FG-001',  'Kursi Kantor Ergonomis',    'Finished good — office chair',
        '{"LotTracking":false,"SerialTracking":true,"ExpiryTracking":false,"ShelfLifeDays":0}', '{"ReorderPoint":20,"ReorderQty":50}',
        '{"Length":60,"Width":60,"Height":110,"Unit":"CM"}', '{"Value":12.5,"Unit":"KG"}', '{"Value":0.396,"Unit":"M3"}', 'ACTIVE', 1, NOW(), NOW()),
    ('a8000000-0000-4000-a000-000000000002', 'a0000000-0000-4000-a000-000000000001', 'a6000000-0000-4000-a000-000000000002', 'a7000000-0000-4000-a000-000000000002', '0a000000-0000-4000-a000-000000000003', 'ALP-RM-001',  'Pelat Baja 2mm',            'Raw material — steel sheet',
        '{"LotTracking":true,"SerialTracking":false,"ExpiryTracking":false,"ShelfLifeDays":0}',  '{"ReorderPoint":500,"ReorderQty":1000}',
        '{"Length":200,"Width":100,"Height":0.2,"Unit":"CM"}', '{"Value":31.4,"Unit":"KG"}', '{"Value":0.004,"Unit":"M3"}', 'ACTIVE', 1, NOW(), NOW()),
    ('a8000000-0000-4000-a000-000000000003', 'a0000000-0000-4000-a000-000000000001', 'a6000000-0000-4000-a000-000000000003', 'a7000000-0000-4000-a000-000000000001', '0a000000-0000-4000-a000-000000000002', 'ALP-PKG-001', 'Kardus Karton 60x40x40',    'Packaging — carton box',
        '{"LotTracking":false,"SerialTracking":false,"ExpiryTracking":false,"ShelfLifeDays":0}', '{"ReorderPoint":200,"ReorderQty":500}',
        '{"Length":60,"Width":40,"Height":40,"Unit":"CM"}', '{"Value":0.6,"Unit":"KG"}', '{"Value":0.096,"Unit":"M3"}', 'ACTIVE', 1, NOW(), NOW()),
    ('a8000000-0000-4000-a000-000000000004', 'a0000000-0000-4000-a000-000000000001', 'a6000000-0000-4000-a000-000000000004', 'a7000000-0000-4000-a000-000000000002', '0a000000-0000-4000-a000-000000000001', 'ALP-SP-001',  'Bearing 6204 ZZ',           'Spare part — bearing',
        '{"LotTracking":true,"SerialTracking":false,"ExpiryTracking":false,"ShelfLifeDays":0}',  '{"ReorderPoint":50,"ReorderQty":100}',
        '{"Length":4.7,"Width":4.7,"Height":1.4,"Unit":"CM"}', '{"Value":0.106,"Unit":"KG"}', '{"Value":0.00003,"Unit":"M3"}', 'ACTIVE', 1, NOW(), NOW()),

    ('b8000000-0000-4000-b000-000000000001', 'b0000000-0000-4000-b000-000000000001', 'b6000000-0000-4000-b000-000000000001', 'b7000000-0000-4000-b000-000000000001', '0a000000-0000-4000-a000-000000000002', 'BET-FG-001',  'Minyak Goreng 2L (Karton)', 'Finished good — cooking oil carton',
        '{"LotTracking":true,"SerialTracking":false,"ExpiryTracking":true,"ShelfLifeDays":540}', '{"ReorderPoint":100,"ReorderQty":300}',
        '{"Length":40,"Width":30,"Height":25,"Unit":"CM"}', '{"Value":12.2,"Unit":"KG"}', '{"Value":0.03,"Unit":"M3"}', 'ACTIVE', 1, NOW(), NOW()),
    ('b8000000-0000-4000-b000-000000000002', 'b0000000-0000-4000-b000-000000000001', 'b6000000-0000-4000-b000-000000000002', 'b7000000-0000-4000-b000-000000000002', '0a000000-0000-4000-a000-000000000006', 'BET-RM-001',  'Gula Pasir Curah 50kg',     'Raw material — bulk sugar',
        '{"LotTracking":true,"SerialTracking":false,"ExpiryTracking":true,"ShelfLifeDays":730}', '{"ReorderPoint":40,"ReorderQty":120}',
        '{"Length":80,"Width":50,"Height":15,"Unit":"CM"}', '{"Value":50,"Unit":"KG"}', '{"Value":0.06,"Unit":"M3"}', 'ACTIVE', 1, NOW(), NOW()),
    ('b8000000-0000-4000-b000-000000000003', 'b0000000-0000-4000-b000-000000000001', 'b6000000-0000-4000-b000-000000000003', 'b7000000-0000-4000-b000-000000000001', '0a000000-0000-4000-a000-000000000005', 'BET-PKG-001', 'Plastik Wrap 50cm',         'Packaging — stretch wrap',
        '{"LotTracking":false,"SerialTracking":false,"ExpiryTracking":false,"ShelfLifeDays":0}', '{"ReorderPoint":30,"ReorderQty":60}',
        '{"Length":50,"Width":20,"Height":20,"Unit":"CM"}', '{"Value":2.4,"Unit":"KG"}', '{"Value":0.02,"Unit":"M3"}', 'ACTIVE', 1, NOW(), NOW()),
    ('b8000000-0000-4000-b000-000000000004', 'b0000000-0000-4000-b000-000000000001', 'b6000000-0000-4000-b000-000000000004', 'b7000000-0000-4000-b000-000000000002', '0a000000-0000-4000-a000-000000000001', 'BET-SP-001',  'Roda Troli 5 inci',         'Spare part — trolley wheel',
        '{"LotTracking":false,"SerialTracking":false,"ExpiryTracking":false,"ShelfLifeDays":0}', '{"ReorderPoint":25,"ReorderQty":50}',
        '{"Length":13,"Width":5,"Height":13,"Unit":"CM"}', '{"Value":0.45,"Unit":"KG"}', '{"Value":0.0008,"Unit":"M3"}', 'ACTIVE', 1, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

INSERT INTO product_barcodes (id, product_id, code, type) VALUES
    ('a9000000-0000-4000-a000-000000000001', 'a8000000-0000-4000-a000-000000000001', '8991234500011', 'EAN13'),
    ('a9000000-0000-4000-a000-000000000002', 'a8000000-0000-4000-a000-000000000003', '8991234500035', 'EAN13'),
    ('b9000000-0000-4000-b000-000000000001', 'b8000000-0000-4000-b000-000000000001', '8997654300012', 'EAN13'),
    ('b9000000-0000-4000-b000-000000000002', 'b8000000-0000-4000-b000-000000000004', '8997654300043', 'EAN13')
ON CONFLICT (id) DO NOTHING;

-- Alternate units: a carton of chairs, and a box of bearings.
INSERT INTO product_alternate_uoms (id, product_id, uom_id, factor) VALUES
    ('aa000000-0000-4000-a000-000000000001', 'a8000000-0000-4000-a000-000000000001', '0a000000-0000-4000-a000-000000000002', 4.000000),
    ('aa000000-0000-4000-a000-000000000002', 'a8000000-0000-4000-a000-000000000004', '0a000000-0000-4000-a000-000000000002', 100.000000),
    ('ba000000-0000-4000-b000-000000000001', 'b8000000-0000-4000-b000-000000000004', '0a000000-0000-4000-a000-000000000002', 24.000000)
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Suppliers and customers
-- ---------------------------------------------------------------------------

INSERT INTO suppliers (id, company_id, code, name, email, phone, tax_number, address, city, province, country, postal_code, status, version, created_by, updated_by, created_at, updated_at) VALUES
    ('ab000000-0000-4000-a000-000000000001', 'a0000000-0000-4000-a000-000000000001', 'ALP-SUP-01', 'PT Baja Perkasa Nusantara', 'sales@bajaperkasa.co.id', '+62-21-4567-8901', '01.234.567.8-011.000', 'Jl. Industri Raya No. 45', 'Bekasi',  'Jawa Barat', 'ID', '17530', 'ACTIVE', 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('ab000000-0000-4000-a000-000000000002', 'a0000000-0000-4000-a000-000000000001', 'ALP-SUP-02', 'CV Kemasan Jaya',           'order@kemasanjaya.co.id','+62-21-4567-8902', '02.345.678.9-012.000', 'Jl. Cakung Cilincing No. 8', 'Jakarta', 'DKI Jakarta','ID', '14140', 'ACTIVE', 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('bb000000-0000-4000-b000-000000000001', 'b0000000-0000-4000-b000-000000000001', 'BET-SUP-01', 'PT Sumber Pangan Sejahtera','sales@sumberpangan.co.id','+62-21-7788-9901','03.456.789.0-013.000', 'Jl. Daan Mogot KM 18',     'Tangerang','Banten',    'ID', '15122', 'ACTIVE', 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW()),
    ('bb000000-0000-4000-b000-000000000002', 'b0000000-0000-4000-b000-000000000001', 'BET-SUP-02', 'PT Gula Manis Indonesia',   'po@gulamanis.co.id',      '+62-21-7788-9902','04.567.890.1-014.000', 'Jl. Raya Bekasi KM 22',    'Bekasi',   'Jawa Barat','ID', '17111', 'ACTIVE', 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

INSERT INTO customers (id, company_id, code, name, email, phone, tax_number, address, city, province, country, postal_code, status, version, created_by, updated_by, created_at, updated_at) VALUES
    ('ac000000-0000-4000-a000-000000000001', 'a0000000-0000-4000-a000-000000000001', 'ALP-CUS-01', 'PT Kantor Modern Indonesia','purchasing@kantormodern.co.id','+62-21-2233-4455','05.678.901.2-015.000','Jl. Sudirman Kav. 52',    'Jakarta',  'DKI Jakarta','ID','12190','ACTIVE', 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('ac000000-0000-4000-a000-000000000002', 'a0000000-0000-4000-a000-000000000001', 'ALP-CUS-02', 'CV Mitra Furnitur',         'admin@mitrafurnitur.co.id',   '+62-31-5566-7788','06.789.012.3-016.000','Jl. Rungkut Industri No. 3','Surabaya','Jawa Timur', 'ID','60293','ACTIVE', 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('bc000000-0000-4000-b000-000000000001', 'b0000000-0000-4000-b000-000000000001', 'BET-CUS-01', 'PT Ritel Sejahtera Abadi',  'buyer@ritelsejahtera.co.id',  '+62-21-9900-1122','07.890.123.4-017.000','Jl. Gatot Subroto Kav. 10','Jakarta', 'DKI Jakarta','ID','12930','ACTIVE', 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW()),
    ('bc000000-0000-4000-b000-000000000002', 'b0000000-0000-4000-b000-000000000001', 'BET-CUS-02', 'Toko Sembako Berkah',       'berkah.toko@gmail.com',       '+62-812-3456-7890','',                  'Jl. Pasar Minggu No. 21', 'Jakarta',  'DKI Jakarta','ID','12520','ACTIVE', 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Inventory positions
-- ---------------------------------------------------------------------------
--
-- Balances are chosen to exercise every bucket a Flutter screen must render:
-- plain available stock, stock partly reserved, stock hard-allocated to a task,
-- and stock held in quarantine.

INSERT INTO inventory_positions (id, company_id, warehouse_id, location_id, product_id, tracking_type, lot_number, serial_number, available, reserved, allocated, quarantined, version, created_by, updated_by, created_at, updated_at) VALUES
    -- Alpha: untracked packaging, lot-tracked steel, lot-tracked bearings (part quarantined)
    ('ad000000-0000-4000-a000-000000000001', 'a0000000-0000-4000-a000-000000000001', 'a4000000-0000-4000-a000-000000000001', 'a5000000-0000-4000-a000-000000000001', 'a8000000-0000-4000-a000-000000000003', 'NONE', NULL,          NULL, 480,  20,  0,  0, 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('ad000000-0000-4000-a000-000000000002', 'a0000000-0000-4000-a000-000000000001', 'a4000000-0000-4000-a000-000000000003', 'a5000000-0000-4000-a000-000000000002', 'a8000000-0000-4000-a000-000000000002', 'LOT',  'LOT-BAJA-2601', NULL, 850, 100, 50,  0, 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    ('ad000000-0000-4000-a000-000000000003', 'a0000000-0000-4000-a000-000000000001', 'a4000000-0000-4000-a000-000000000001', 'a5000000-0000-4000-a000-000000000003', 'a8000000-0000-4000-a000-000000000004', 'LOT',  'LOT-BRG-2602',  NULL, 140,   0,  0, 20, 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),
    -- Alpha: a serialised finished chair — exactly one unit, as the aggregate requires
    ('ad000000-0000-4000-a000-000000000004', 'a0000000-0000-4000-a000-000000000001', 'a4000000-0000-4000-a000-000000000002', 'a5000000-0000-4000-a000-000000000004', 'a8000000-0000-4000-a000-000000000001', 'SERIAL', NULL, 'ALP-CHAIR-SN-0001', 1, 0, 0, 0, 1, 'a1000000-0000-4000-a000-000000000001', 'a1000000-0000-4000-a000-000000000001', NOW(), NOW()),

    -- Beta: lot-tracked oil and sugar, untracked wrap and wheels
    ('bd000000-0000-4000-b000-000000000001', 'b0000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'b5000000-0000-4000-b000-000000000001', 'b8000000-0000-4000-b000-000000000001', 'LOT',  'LOT-MG-2605', NULL, 320,  60, 20,  0, 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW()),
    ('bd000000-0000-4000-b000-000000000002', 'b0000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'b5000000-0000-4000-b000-000000000002', 'b8000000-0000-4000-b000-000000000002', 'LOT',  'LOT-GP-2606', NULL,  95,   0,  0, 10, 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW()),
    ('bd000000-0000-4000-b000-000000000003', 'b0000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'b5000000-0000-4000-b000-000000000003', 'b8000000-0000-4000-b000-000000000003', 'NONE', NULL,          NULL,  60,   5,  0,  0, 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW()),
    ('bd000000-0000-4000-b000-000000000004', 'b0000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000002', 'b5000000-0000-4000-b000-000000000004', 'b8000000-0000-4000-b000-000000000004', 'NONE', NULL,          NULL, 240,   0, 40,  0, 1, 'b1000000-0000-4000-b000-000000000001', 'b1000000-0000-4000-b000-000000000001', NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Inventory ledger — the history that produced the balances above
-- ---------------------------------------------------------------------------
--
-- Each chain is written so the delta columns satisfy the table's CHECK
-- (delta = after - before) and the final AFTER snapshot equals the position's
-- current balances. A ledger that did not reconcile with its position would be
-- worse than none at all.

INSERT INTO inventory_ledger_entries (id, company_id, position_id, product_id, warehouse_id, location_id, lot_number, serial_number, movement_type, reference_type, reference_id, document_number, reason, actor_id,
    before_available, before_reserved, before_allocated, before_quarantined,
    after_available,  after_reserved,  after_allocated,  after_quarantined,
    delta_available,  delta_reserved,  delta_allocated,  delta_quarantined, delta_on_hand,
    occurred_at, version, created_at, updated_at) VALUES

    -- Alpha packaging: opening balance 500, then 20 reserved for an order.
    ('ae000000-0000-4000-a000-000000000001', 'a0000000-0000-4000-a000-000000000001', 'ad000000-0000-4000-a000-000000000001', 'a8000000-0000-4000-a000-000000000003', 'a4000000-0000-4000-a000-000000000001', 'a5000000-0000-4000-a000-000000000001', NULL, NULL,
        'INITIAL_BALANCE', 'STOCK_OPENING', NULL, 'OPN-ALP-0001', 'Saldo awal implementasi', 'a1000000-0000-4000-a000-000000000001',
        0,0,0,0, 500,0,0,0, 500,0,0,0, 500, NOW() - INTERVAL '30 days', 1, NOW(), NOW()),
    ('ae000000-0000-4000-a000-000000000002', 'a0000000-0000-4000-a000-000000000001', 'ad000000-0000-4000-a000-000000000001', 'a8000000-0000-4000-a000-000000000003', 'a4000000-0000-4000-a000-000000000001', 'a5000000-0000-4000-a000-000000000001', NULL, NULL,
        'RESERVATION', 'SALES_ORDER', NULL, 'SO-ALP-1001', 'Reservasi untuk pengiriman', 'a1000000-0000-4000-a000-000000000002',
        500,0,0,0, 480,20,0,0, -20,20,0,0, 0, NOW() - INTERVAL '3 days', 1, NOW(), NOW()),

    -- Alpha steel: receipt of 1000, then 100 reserved and 50 allocated to a work order.
    ('ae000000-0000-4000-a000-000000000003', 'a0000000-0000-4000-a000-000000000001', 'ad000000-0000-4000-a000-000000000002', 'a8000000-0000-4000-a000-000000000002', 'a4000000-0000-4000-a000-000000000003', 'a5000000-0000-4000-a000-000000000002', 'LOT-BAJA-2601', NULL,
        'INBOUND', 'PURCHASE_ORDER', NULL, 'PO-ALP-2001', 'Penerimaan pelat baja', 'a1000000-0000-4000-a000-000000000003',
        0,0,0,0, 1000,0,0,0, 1000,0,0,0, 1000, NOW() - INTERVAL '20 days', 1, NOW(), NOW()),
    ('ae000000-0000-4000-a000-000000000004', 'a0000000-0000-4000-a000-000000000001', 'ad000000-0000-4000-a000-000000000002', 'a8000000-0000-4000-a000-000000000002', 'a4000000-0000-4000-a000-000000000003', 'a5000000-0000-4000-a000-000000000002', 'LOT-BAJA-2601', NULL,
        'RESERVATION', 'WORK_ORDER', NULL, 'WO-ALP-3001', 'Reservasi produksi', 'a1000000-0000-4000-a000-000000000002',
        1000,0,0,0, 850,150,0,0, -150,150,0,0, 0, NOW() - INTERVAL '10 days', 1, NOW(), NOW()),
    ('ae000000-0000-4000-a000-000000000005', 'a0000000-0000-4000-a000-000000000001', 'ad000000-0000-4000-a000-000000000002', 'a8000000-0000-4000-a000-000000000002', 'a4000000-0000-4000-a000-000000000003', 'a5000000-0000-4000-a000-000000000002', 'LOT-BAJA-2601', NULL,
        'ALLOCATION', 'WORK_ORDER', NULL, 'WO-ALP-3001', 'Alokasi ke tugas potong', 'a1000000-0000-4000-a000-000000000003',
        850,150,0,0, 850,100,50,0, 0,-50,50,0, 0, NOW() - INTERVAL '2 days', 1, NOW(), NOW()),

    -- Alpha bearings: receipt of 160, then 20 quarantined after inspection.
    ('ae000000-0000-4000-a000-000000000006', 'a0000000-0000-4000-a000-000000000001', 'ad000000-0000-4000-a000-000000000003', 'a8000000-0000-4000-a000-000000000004', 'a4000000-0000-4000-a000-000000000001', 'a5000000-0000-4000-a000-000000000003', 'LOT-BRG-2602', NULL,
        'INBOUND', 'PURCHASE_ORDER', NULL, 'PO-ALP-2002', 'Penerimaan bearing', 'a1000000-0000-4000-a000-000000000003',
        0,0,0,0, 160,0,0,0, 160,0,0,0, 160, NOW() - INTERVAL '15 days', 1, NOW(), NOW()),
    ('ae000000-0000-4000-a000-000000000007', 'a0000000-0000-4000-a000-000000000001', 'ad000000-0000-4000-a000-000000000003', 'a8000000-0000-4000-a000-000000000004', 'a4000000-0000-4000-a000-000000000001', 'a5000000-0000-4000-a000-000000000003', 'LOT-BRG-2602', NULL,
        'QUARANTINE', 'QC_INSPECTION', NULL, 'QC-ALP-4001', 'Karat permukaan, menunggu keputusan QC', 'a1000000-0000-4000-a000-000000000002',
        160,0,0,0, 140,0,0,20, -20,0,0,20, 0, NOW() - INTERVAL '5 days', 1, NOW(), NOW()),

    -- Alpha serialised chair: a single unit received.
    ('ae000000-0000-4000-a000-000000000008', 'a0000000-0000-4000-a000-000000000001', 'ad000000-0000-4000-a000-000000000004', 'a8000000-0000-4000-a000-000000000001', 'a4000000-0000-4000-a000-000000000002', 'a5000000-0000-4000-a000-000000000004', NULL, 'ALP-CHAIR-SN-0001',
        'INBOUND', 'PRODUCTION', NULL, 'PRD-ALP-5001', 'Hasil produksi unit bernomor seri', 'a1000000-0000-4000-a000-000000000003',
        0,0,0,0, 1,0,0,0, 1,0,0,0, 1, NOW() - INTERVAL '7 days', 1, NOW(), NOW()),

    -- Beta oil: receipt 400, reserve 60, allocate 20, then issue 80 against a delivery.
    ('be000000-0000-4000-b000-000000000001', 'b0000000-0000-4000-b000-000000000001', 'bd000000-0000-4000-b000-000000000001', 'b8000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'b5000000-0000-4000-b000-000000000001', 'LOT-MG-2605', NULL,
        'INBOUND', 'PURCHASE_ORDER', NULL, 'PO-BET-2001', 'Penerimaan minyak goreng', 'b1000000-0000-4000-b000-000000000003',
        0,0,0,0, 400,0,0,0, 400,0,0,0, 400, NOW() - INTERVAL '25 days', 1, NOW(), NOW()),
    ('be000000-0000-4000-b000-000000000002', 'b0000000-0000-4000-b000-000000000001', 'bd000000-0000-4000-b000-000000000001', 'b8000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'b5000000-0000-4000-b000-000000000001', 'LOT-MG-2605', NULL,
        'OUTBOUND', 'DELIVERY_ORDER', NULL, 'DO-BET-6001', 'Pengiriman ke PT Ritel Sejahtera Abadi', 'b1000000-0000-4000-b000-000000000003',
        400,0,0,0, 320,0,0,0, -80,0,0,0, -80, NOW() - INTERVAL '12 days', 1, NOW(), NOW()),
    ('be000000-0000-4000-b000-000000000003', 'b0000000-0000-4000-b000-000000000001', 'bd000000-0000-4000-b000-000000000001', 'b8000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'b5000000-0000-4000-b000-000000000001', 'LOT-MG-2605', NULL,
        'RESERVATION', 'SALES_ORDER', NULL, 'SO-BET-1002', 'Reservasi pesanan ritel', 'b1000000-0000-4000-b000-000000000002',
        320,0,0,0, 240,80,0,0, -80,80,0,0, 0, NOW() - INTERVAL '4 days', 1, NOW(), NOW()),
    ('be000000-0000-4000-b000-000000000004', 'b0000000-0000-4000-b000-000000000001', 'bd000000-0000-4000-b000-000000000001', 'b8000000-0000-4000-b000-000000000001', 'b4000000-0000-4000-b000-000000000001', 'b5000000-0000-4000-b000-000000000001', 'LOT-MG-2605', NULL,
        'ALLOCATION', 'SALES_ORDER', NULL, 'SO-BET-1002', 'Alokasi ke tugas pengambilan', 'b1000000-0000-4000-b000-000000000003',
        240,80,0,0, 320,60,20,0, 80,-20,20,0, 80, NOW() - INTERVAL '1 days', 1, NOW(), NOW()),

    -- Beta sugar: opening 100, cycle count found 95, then 10 quarantined for damp sacks.
    ('be000000-0000-4000-b000-000000000005', 'b0000000-0000-4000-b000-000000000001', 'bd000000-0000-4000-b000-000000000002', 'b8000000-0000-4000-b000-000000000002', 'b4000000-0000-4000-b000-000000000001', 'b5000000-0000-4000-b000-000000000002', 'LOT-GP-2606', NULL,
        'INITIAL_BALANCE', 'STOCK_OPENING', NULL, 'OPN-BET-0001', 'Saldo awal implementasi', 'b1000000-0000-4000-b000-000000000001',
        0,0,0,0, 110,0,0,0, 110,0,0,0, 110, NOW() - INTERVAL '30 days', 1, NOW(), NOW()),
    ('be000000-0000-4000-b000-000000000006', 'b0000000-0000-4000-b000-000000000001', 'bd000000-0000-4000-b000-000000000002', 'b8000000-0000-4000-b000-000000000002', 'b4000000-0000-4000-b000-000000000001', 'b5000000-0000-4000-b000-000000000002', 'LOT-GP-2606', NULL,
        'CYCLE_COUNT', 'CYCLE_COUNT', NULL, 'CC-BET-7001', 'Selisih hasil hitung fisik', 'b1000000-0000-4000-b000-000000000002',
        110,0,0,0, 105,0,0,0, -5,0,0,0, -5, NOW() - INTERVAL '9 days', 1, NOW(), NOW()),
    ('be000000-0000-4000-b000-000000000007', 'b0000000-0000-4000-b000-000000000001', 'bd000000-0000-4000-b000-000000000002', 'b8000000-0000-4000-b000-000000000002', 'b4000000-0000-4000-b000-000000000001', 'b5000000-0000-4000-b000-000000000002', 'LOT-GP-2606', NULL,
        'QUARANTINE', 'QC_INSPECTION', NULL, 'QC-BET-4002', 'Karung lembab, menunggu pemeriksaan', 'b1000000-0000-4000-b000-000000000002',
        105,0,0,0, 95,0,0,10, -10,0,0,10, 0, NOW() - INTERVAL '2 days', 1, NOW(), NOW()),

    -- Beta wrap and wheels: simple opening balances plus one reservation/allocation.
    ('be000000-0000-4000-b000-000000000008', 'b0000000-0000-4000-b000-000000000001', 'bd000000-0000-4000-b000-000000000003', 'b8000000-0000-4000-b000-000000000003', 'b4000000-0000-4000-b000-000000000001', 'b5000000-0000-4000-b000-000000000003', NULL, NULL,
        'INITIAL_BALANCE', 'STOCK_OPENING', NULL, 'OPN-BET-0002', 'Saldo awal implementasi', 'b1000000-0000-4000-b000-000000000001',
        0,0,0,0, 65,0,0,0, 65,0,0,0, 65, NOW() - INTERVAL '30 days', 1, NOW(), NOW()),
    ('be000000-0000-4000-b000-000000000009', 'b0000000-0000-4000-b000-000000000001', 'bd000000-0000-4000-b000-000000000003', 'b8000000-0000-4000-b000-000000000003', 'b4000000-0000-4000-b000-000000000001', 'b5000000-0000-4000-b000-000000000003', NULL, NULL,
        'RESERVATION', 'SALES_ORDER', NULL, 'SO-BET-1003', 'Reservasi pengemasan', 'b1000000-0000-4000-b000-000000000002',
        65,0,0,0, 60,5,0,0, -5,5,0,0, 0, NOW() - INTERVAL '1 days', 1, NOW(), NOW()),
    ('be000000-0000-4000-b000-00000000000a', 'b0000000-0000-4000-b000-000000000001', 'bd000000-0000-4000-b000-000000000004', 'b8000000-0000-4000-b000-000000000004', 'b4000000-0000-4000-b000-000000000002', 'b5000000-0000-4000-b000-000000000004', NULL, NULL,
        'INITIAL_BALANCE', 'STOCK_OPENING', NULL, 'OPN-BET-0003', 'Saldo awal implementasi', 'b1000000-0000-4000-b000-000000000001',
        0,0,0,0, 280,0,0,0, 280,0,0,0, 280, NOW() - INTERVAL '30 days', 1, NOW(), NOW()),
    ('be000000-0000-4000-b000-00000000000b', 'b0000000-0000-4000-b000-000000000001', 'bd000000-0000-4000-b000-000000000004', 'b8000000-0000-4000-b000-000000000004', 'b4000000-0000-4000-b000-000000000002', 'b5000000-0000-4000-b000-000000000004', NULL, NULL,
        'RESERVATION', 'SALES_ORDER', NULL, 'SO-BET-1004', 'Reservasi suku cadang', 'b1000000-0000-4000-b000-000000000002',
        280,0,0,0, 240,40,0,0, -40,40,0,0, 0, NOW() - INTERVAL '6 days', 1, NOW(), NOW()),
    ('be000000-0000-4000-b000-00000000000c', 'b0000000-0000-4000-b000-000000000001', 'bd000000-0000-4000-b000-000000000004', 'b8000000-0000-4000-b000-000000000004', 'b4000000-0000-4000-b000-000000000002', 'b5000000-0000-4000-b000-000000000004', NULL, NULL,
        'ALLOCATION', 'SALES_ORDER', NULL, 'SO-BET-1004', 'Alokasi ke tugas pengambilan', 'b1000000-0000-4000-b000-000000000003',
        240,40,0,0, 240,0,40,0, 0,-40,40,0, 0, NOW() - INTERVAL '5 days', 1, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;
