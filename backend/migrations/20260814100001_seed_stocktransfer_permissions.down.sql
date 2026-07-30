-- 20260814100001: seed_stocktransfer_permissions (down)
--
-- Grants first: role_permissions references permissions.
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('stocktransfer.read', 'stocktransfer.create', 'stocktransfer.update',
                   'stocktransfer.confirm', 'stocktransfer.complete', 'stocktransfer.cancel')
);

DELETE FROM permissions
WHERE code IN ('stocktransfer.read', 'stocktransfer.create', 'stocktransfer.update',
               'stocktransfer.confirm', 'stocktransfer.complete', 'stocktransfer.cancel');
