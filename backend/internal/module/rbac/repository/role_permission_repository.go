package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/infra/postgres"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// RolePermissionRepository manages the grants between roles and permissions.
//
// Methods are scoped by ROLE rather than by company. The role id is itself
// tenant-scoped — every caller obtains it from a company-filtered role query —
// so a second company filter here would join roles again for no added safety.
// The foreign key guarantees a grant cannot outlive its role.
type RolePermissionRepository interface {
	// Grant creates the missing grants for a role. Idempotent: codes the role
	// already holds are left alone rather than duplicated.
	Grant(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID) (int64, error)

	// Revoke soft-deletes the given grants.
	//
	// Soft rather than hard: "who revoked what, and when" is exactly the
	// question an access-control audit asks, and a hard delete leaves no
	// evidence the permission was ever held.
	Revoke(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID, at time.Time) (int64, error)

	// PermissionIDs returns the permission ids a role currently holds.
	PermissionIDs(ctx context.Context, roleID uuid.UUID) ([]uuid.UUID, error)
}

type rolePermissionRepository struct {
	*base.Base[entity.RolePermission, *entity.RolePermission]
	ids port.IDGenerator
}

var _ RolePermissionRepository = (*rolePermissionRepository)(nil)

// NewRolePermissionRepository builds the repository.
func NewRolePermissionRepository(db *gorm.DB, ids port.IDGenerator) RolePermissionRepository {
	return &rolePermissionRepository{
		Base: base.New[entity.RolePermission, *entity.RolePermission](
			db, ids, "rbac.role_permission_repository"),
		ids: ids,
	}
}

func (r *rolePermissionRepository) PermissionIDs(
	ctx context.Context, roleID uuid.UUID,
) ([]uuid.UUID, error) {
	var ids []uuid.UUID

	err := r.Base.DB(ctx).
		Model(&entity.RolePermission{}).
		Where("role_id = ?", roleID).
		Where("deleted_at IS NULL").
		Pluck("permission_id", &ids).Error
	if err != nil {
		return nil, postgres.TranslateError(
			err, "rbac.role_permission_repository.PermissionIDs")
	}

	return ids, nil
}

// Grant inserts the missing grants.
//
// It resurrects soft-deleted rows rather than inserting alongside them. A
// previously revoked grant still occupies the (role_id, permission_id) pair in
// the table, and although the partial unique index permits a second live row,
// having two rows for one grant would make the revoke path ambiguous. Clearing
// deleted_at reuses the row and preserves its history in updated_at.
func (r *rolePermissionRepository) Grant(
	ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID,
) (int64, error) {
	if len(permissionIDs) == 0 {
		return 0, nil
	}

	db := r.Base.DB(ctx)
	var granted int64

	// Revive any soft-deleted rows for these permissions first.
	revived := db.
		Unscoped().
		Model(&entity.RolePermission{}).
		Where("role_id = ?", roleID).
		Where("permission_id IN ?", permissionIDs).
		Where("deleted_at IS NOT NULL").
		Updates(map[string]any{"deleted_at": nil})
	if revived.Error != nil {
		return 0, postgres.TranslateError(
			revived.Error, "rbac.role_permission_repository.Grant.revive")
	}
	granted += revived.RowsAffected

	// Then insert the ones that have never existed.
	existing, err := r.PermissionIDs(ctx, roleID)
	if err != nil {
		return 0, err
	}

	held := make(map[uuid.UUID]struct{}, len(existing))
	for _, id := range existing {
		held[id] = struct{}{}
	}

	rows := make([]entity.RolePermission, 0, len(permissionIDs))
	for _, permissionID := range permissionIDs {
		if _, ok := held[permissionID]; ok {
			continue
		}
		row := entity.RolePermission{RoleID: roleID, PermissionID: permissionID}
		row.SetID(r.ids.NewID())
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		return granted, nil
	}

	created := db.Create(&rows)
	if created.Error != nil {
		return 0, postgres.TranslateError(
			created.Error, "rbac.role_permission_repository.Grant")
	}

	return granted + created.RowsAffected, nil
}

func (r *rolePermissionRepository) Revoke(
	ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID, at time.Time,
) (int64, error) {
	if len(permissionIDs) == 0 {
		return 0, nil
	}

	result := r.Base.DB(ctx).
		Model(&entity.RolePermission{}).
		Where("role_id = ?", roleID).
		Where("permission_id IN ?", permissionIDs).
		Where("deleted_at IS NULL").
		Update("deleted_at", at)

	if result.Error != nil {
		return 0, postgres.TranslateError(
			result.Error, "rbac.role_permission_repository.Revoke")
	}

	return result.RowsAffected, nil
}
