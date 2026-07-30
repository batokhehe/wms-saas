-- 20260816100002: seed_goodsreceipt_permissions (down)
-- Grants first: role_permissions references permissions.
DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE module = 'goodsreceipt');

DELETE FROM permissions WHERE module = 'goodsreceipt';
