-- 20260809100000: seed_catalog_permissions (down)
-- Grants first: role_permissions holds the foreign key into permissions.

DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE code IN
    ('category.read','category.create','category.update','category.delete',
     'brand.read','brand.create','brand.update','brand.delete','product.delete'));

DELETE FROM permissions WHERE code IN
    ('category.read','category.create','category.update','category.delete',
     'brand.read','brand.create','brand.update','brand.delete','product.delete');
