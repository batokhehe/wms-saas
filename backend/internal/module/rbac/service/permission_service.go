package service

import (
	"context"

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
)

// PermissionService reads the permission catalogue and the caller's own grants.
//
// It offers no write operations. "System permissions cannot be modified" is
// enforced structurally — the repository has no Create, Update or Delete, so
// there is nothing for a service method to call — rather than by a check
// somebody could forget to write. The catalogue changes only by migration.
type PermissionService struct {
	permissions repository.PermissionRepository
	evaluator   *Evaluator
}

// NewPermissionService builds the service.
func NewPermissionService(
	permissions repository.PermissionRepository,
	evaluator *Evaluator,
) *PermissionService {
	return &PermissionService{permissions: permissions, evaluator: evaluator}
}

// List returns the global permission catalogue.
//
// It requires a company context even though permissions are global. That is
// deliberate: the endpoint exists to populate a role editor, which is a
// tenant-scoped screen, and requiring the context keeps every RBAC route behind
// the same guard rather than creating one exception a reader has to notice.
func (s *PermissionService) List(
	ctx context.Context, query dto.ListPermissionsQuery,
) ([]dto.PermissionResponse, error) {
	if _, err := appcontext.From(ctx).RequireTenant(); err != nil {
		return nil, err
	}

	permissions, err := s.permissions.List(ctx, query.Module)
	if err != nil {
		return nil, err
	}

	return mapper.ToPermissionResponses(permissions), nil
}

// Mine reports what the caller may do in the active company.
//
// This backs a client hiding buttons the user cannot press. It is a
// convenience, never a security boundary: the server enforces every permission
// independently through middleware, and a client that ignored this response
// entirely would gain nothing.
func (s *PermissionService) Mine(ctx context.Context) (dto.MyPermissionsResponse, error) {
	rc := appcontext.From(ctx)

	companyID, err := rc.RequireTenant()
	if err != nil {
		return dto.MyPermissionsResponse{}, err
	}

	set, err := s.evaluator.ResolveSet(ctx, companyID, rc.Role)
	if err != nil {
		return dto.MyPermissionsResponse{}, err
	}

	return mapper.ToMyPermissionsResponse(rc.Role, set), nil
}
