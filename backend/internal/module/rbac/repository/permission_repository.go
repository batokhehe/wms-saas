package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/infra/postgres"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// PermissionRepository reads the global permission catalogue.
//
// Note the absence of a companyID on every method, and the absence of any
// write method. Both are deliberate:
//
//   - Permissions are global, so there is no tenant to scope by. This is the
//     documented exception to RepositoryConvention §3, alongside the auth
//     module's user repository.
//   - The catalogue is seeded by migration and is immutable at runtime. "System
//     permissions cannot be modified" is enforced structurally — there is no
//     Create, Update or Delete to call — rather than by a check somebody could
//     forget to write.
type PermissionRepository interface {
	List(ctx context.Context, module string) ([]entity.Permission, error)

	// FindByCodes resolves codes to rows, for translating a client's requested
	// permission set into the ids stored in role_permissions.
	FindByCodes(ctx context.Context, codes []entity.Code) ([]entity.Permission, error)

	// CodesByRole returns the permission codes granted to each of the given
	// roles, in ONE query.
	//
	// Taking a slice rather than a single role id is what keeps the role list
	// endpoint free of the N+1 problem: rendering a page of roles with their
	// permissions costs two queries regardless of page size.
	CodesByRole(ctx context.Context, roleIDs []uuid.UUID) (map[uuid.UUID][]entity.Code, error)
}

// permissionRepository does NOT embed the generic base.
//
// Embedding would promote the base's Create, Update and Delete onto this type,
// so the catalogue's immutability would depend on nobody type-asserting past
// the interface. Holding the base as an unexported FIELD makes the guarantee
// structural: the concrete type exposes only the three read methods below, and
// there is no write path to forget to guard.
//
// An integration test asserts this — it caught the embedded version.
type permissionRepository struct {
	repo *base.Base[entity.Permission, *entity.Permission]
}

var _ PermissionRepository = (*permissionRepository)(nil)

// NewPermissionRepository builds the repository.
func NewPermissionRepository(db *gorm.DB, ids port.IDGenerator) PermissionRepository {
	return &permissionRepository{
		repo: base.New[entity.Permission, *entity.Permission](
			db, ids, "rbac.permission_repository"),
	}
}

func (r *permissionRepository) List(
	ctx context.Context, module string,
) ([]entity.Permission, error) {
	scopes := []base.Scope{}
	if module != "" {
		scopes = append(scopes, base.Where("module = ?", module))
	}

	// Ordered so the catalogue renders identically on every call — an unordered
	// list would reshuffle between requests and break client-side diffing.
	scopes = append(scopes, func(db *gorm.DB) *gorm.DB {
		return db.Order("module ASC, code ASC")
	})

	return r.repo.FindMany(ctx, scopes...)
}

func (r *permissionRepository) FindByCodes(
	ctx context.Context, codes []entity.Code,
) ([]entity.Permission, error) {
	if len(codes) == 0 {
		return []entity.Permission{}, nil
	}

	// Converted to strings because GORM cannot bind a []entity.Code into an IN
	// clause — the driver has no encoder for a named string type slice.
	raw := make([]string, 0, len(codes))
	for _, code := range codes {
		raw = append(raw, code.String())
	}

	return r.repo.FindMany(ctx, base.Where("code IN ?", raw))
}

// codeRow is the projection CodesByRole scans into.
type codeRow struct {
	RoleID uuid.UUID
	Code   string
}

func (r *permissionRepository) CodesByRole(
	ctx context.Context, roleIDs []uuid.UUID,
) (map[uuid.UUID][]entity.Code, error) {
	result := make(map[uuid.UUID][]entity.Code, len(roleIDs))
	if len(roleIDs) == 0 {
		return result, nil
	}

	var rows []codeRow

	// A hand-written join rather than a base method: this reads across two
	// tables and projects two columns, which the generic repository cannot
	// express. It goes through the base handle so it still enrols in the
	// caller's transaction, and its error is translated like any other.
	//
	// No tenant filter appears here, and that is correct: role_permissions is
	// reachable only via role_id, and every caller obtains those ids from a
	// company-scoped role query. Adding a company filter would mean joining
	// roles a second time for no additional safety.
	err := r.repo.DB(ctx).
		Table("role_permissions AS rp").
		Select("rp.role_id AS role_id, p.code AS code").
		Joins("JOIN permissions p ON p.id = rp.permission_id AND p.deleted_at IS NULL").
		Where("rp.role_id IN ?", roleIDs).
		Where("rp.deleted_at IS NULL").
		Scan(&rows).Error
	if err != nil {
		return nil, postgres.TranslateError(err, "rbac.permission_repository.CodesByRole")
	}

	for _, row := range rows {
		result[row.RoleID] = append(result[row.RoleID], entity.Code(row.Code))
	}

	return result, nil
}
