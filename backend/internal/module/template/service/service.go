// Package service holds the module's business rules.
//
// LAYER RULE: no gin, no gorm, no http. A service takes context.Context and
// domain/DTO types, and returns domain/DTO types and errors. That is what lets
// the same service be driven by an HTTP handler today, an Asynq worker
// tomorrow, and a table-driven unit test with no infrastructure at all.
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/template/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/template/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/template/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Service implements the module's use cases.
//
// It depends on the repository interface and on ports — never on concrete
// infrastructure. Note there is no *redis.Client, no *asynq.Client and no
// *gorm.DB here: the service asks for a cache, a queue and a transaction
// runner, not for Redis, Asynq and GORM.
type Service struct {
	repo  repository.Repository
	cache port.Cache
	queue port.Queue
	clock port.Clock
	tx    transaction.Manager
}

// New builds the service. Dependencies arrive as constructor arguments rather
// than being resolved from a container, which is what keeps the object graph
// explicit and the type testable.
func New(
	repo repository.Repository,
	cache port.Cache,
	queue port.Queue,
	clock port.Clock,
	tx transaction.Manager,
) *Service {
	return &Service{repo: repo, cache: cache, queue: queue, clock: clock, tx: tx}
}

// tenant extracts the caller's company from the request context.
//
// Every tenant-scoped use case starts with this call. Reading the tenant from
// the context rather than from a request field is what makes cross-tenant
// access impossible to request: the client has no way to influence it.
//
// Until authentication exists, CompanyID is nil and this returns a 401. That is
// the correct placeholder behaviour — it fails closed.
func tenant(ctx context.Context) (uuid.UUID, error) {
	rc := appcontext.From(ctx)

	if !rc.HasTenant() {
		return uuid.Nil, apperror.Unauthorized("A company context is required").
			WithOp("template.service.tenant")
	}

	return *rc.CompanyID, nil
}

// Create is the template for a write use case.
//
// TEMPLATE: the shape is the lesson, not the logic. Resolve the tenant, enforce
// invariants, delegate persistence, map to a DTO.
//
// The check-then-write pair runs inside a transaction. Without one, two
// concurrent requests both see "name is free" and both insert — and the
// uniqueness rule the service appears to enforce is not actually enforced. The
// database unique index is the real guarantee; the transaction is what turns
// the race into a clean CONFLICT instead of two committed rows.
func (s *Service) Create(ctx context.Context, req dto.CreateRequest) (dto.Response, error) {
	companyID, err := tenant(ctx)
	if err != nil {
		return dto.Response{}, err
	}

	var result dto.Response

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		// Business invariants are checked here, not in the handler and not in
		// the repository. This is the layer that owns "what is allowed".
		exists, err := s.repo.ExistsByName(ctx, companyID, req.Name)
		if err != nil {
			return err
		}
		if exists {
			return apperror.Conflict("A resource with this name already exists").
				WithOp("template.service.Create")
		}

		resource := mapper.FromCreateRequest(req, companyID)

		// The repository assigns the ID from the injected generator; the
		// service never calls uuid.New().
		if err := s.repo.Create(ctx, &resource); err != nil {
			return err
		}

		result = mapper.ToResponse(&resource)
		return nil
	})
	if err != nil {
		return dto.Response{}, err
	}

	appcontext.Logger(ctx).Info("resource created")

	return result, nil
}

// Get is the template for a single-item read use case.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (dto.Response, error) {
	companyID, err := tenant(ctx)
	if err != nil {
		return dto.Response{}, err
	}

	resource, err := s.repo.FindByID(ctx, companyID, id)
	if err != nil {
		// No special handling for "not found": the repository already
		// translated it into a NOT_FOUND apperror, and re-wrapping would only
		// obscure the original operation name.
		return dto.Response{}, err
	}

	return mapper.ToResponse(resource), nil
}

// List is the template for a paginated read use case.
//
// It returns a fully-formed page rather than a (slice, total) pair, so no
// caller has to reassemble the metadata.
func (s *Service) List(
	ctx context.Context,
	query dto.ListQuery,
) (pagination.Page[dto.Response], error) {
	companyID, err := tenant(ctx)
	if err != nil {
		return pagination.Page[dto.Response]{}, err
	}

	// Apply resolves the sort key against the endpoint's allow-list and
	// normalises paging. It must run before the query reaches the repository;
	// the base repository refuses an unapplied request.
	if err := query.Request.Apply(dto.SortOptions()); err != nil {
		return pagination.Page[dto.Response]{}, err
	}

	page, err := s.repo.FindAll(ctx, companyID, query)
	if err != nil {
		return pagination.Page[dto.Response]{}, err
	}

	return mapper.ToResponsePage(page), nil
}

// Update is the template for a partial-update use case.
func (s *Service) Update(
	ctx context.Context,
	id uuid.UUID,
	req dto.UpdateRequest,
) (dto.Response, error) {
	companyID, err := tenant(ctx)
	if err != nil {
		return dto.Response{}, err
	}

	var result dto.Response

	// Read-modify-write must be atomic: without a transaction, two concurrent
	// PATCHes both read the same row and the second silently overwrites the
	// first's changes.
	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		resource, err := s.repo.FindByID(ctx, companyID, id)
		if err != nil {
			return err
		}

		// Defence in depth. The repository already filtered by tenant, so this
		// can only fire if that filter is ever broken — which is exactly when a
		// cross-tenant leak would otherwise go unnoticed.
		if !resource.BelongsTo(companyID) {
			return apperror.Forbidden("This resource belongs to another company").
				WithOp("template.service.Update")
		}

		mapper.ApplyUpdateRequest(resource, req)

		if err := s.repo.Update(ctx, resource); err != nil {
			return err
		}

		result = mapper.ToResponse(resource)
		return nil
	})
	if err != nil {
		return dto.Response{}, err
	}

	return result, nil
}

// Delete is the template for a delete use case. It soft-deletes: the row stays
// in the table with deleted_at set. See docs/SoftDeleteConvention.md.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	companyID, err := tenant(ctx)
	if err != nil {
		return err
	}

	return s.repo.Delete(ctx, companyID, id)
}
