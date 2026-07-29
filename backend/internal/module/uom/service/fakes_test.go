package service_test

import (
	"context"
	"strings"
	"sync"

	"github.com/batokhehe/wms-saas/backend/internal/module/uom/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
	"github.com/google/uuid"
)

type fakeRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.UOM
	failOn map[string]error
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		byID:   map[uuid.UUID]*entity.UOM{},
		failOn: map[string]error{},
	}
}

func (r *fakeRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeRepo) Save(_ context.Context, uom *entity.UOM) error {
	if err := r.failOn["Save"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.byID {
		if strings.EqualFold(existing.Code(), uom.Code()) {
			return apperror.Conflict("duplicate code").WithOp("fake.Save")
		}
	}

	r.byID[uom.ID()] = uom
	return nil
}

func (r *fakeRepo) Update(_ context.Context, uom *entity.UOM) error {
	if err := r.failOn["Update"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.byID[uom.ID()]
	if !ok {
		return apperror.NotFound("uom not found").WithOp("fake.Update")
	}

	if existing.Version() != uom.Version() {
		return sharedrepo.ErrConcurrentModification
	}

	// In real system version is handled by DB base repository.
	// For fake simulate increment.
	r.byID[uom.ID()], _ = entity.ReconstituteWithVersion(
		uom.ID(), uom.Code(), uom.Name(), uom.Description(), uom.Status(),
		uom.CreatedAt(), uom.UpdatedAt(), uom.Version()+1,
	)
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, id uuid.UUID) (*entity.UOM, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	uom, ok := r.byID[id]
	if !ok {
		return nil, apperror.NotFound("uom not found").WithOp("fake.FindByID")
	}
	return uom, nil
}

func (r *fakeRepo) FindByCode(_ context.Context, code string) (*entity.UOM, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, uom := range r.byID {
		if strings.EqualFold(uom.Code(), code) {
			return uom, nil
		}
	}
	return nil, apperror.NotFound("uom not found").WithOp("fake.FindByCode")
}

func (r *fakeRepo) List(_ context.Context, filter repository.ListFilter) (pagination.Page[*entity.UOM], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	matched := make([]*entity.UOM, 0)
	for _, uom := range r.byID {
		if filter.Status != "" && uom.Status().Valid() && uom.Status().String() != filter.Status {
			continue
		}
		matched = append(matched, uom)
	}

	return pagination.NewPage(matched, filter.Paging, int64(len(matched))), nil
}

func (r *fakeRepo) Exists(_ context.Context, id uuid.UUID) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.byID[id]
	return ok, nil
}

func (r *fakeRepo) ExistsByCode(_ context.Context, code string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, uom := range r.byID {
		if strings.EqualFold(uom.Code(), code) {
			return true, nil
		}
	}
	return false, nil
}
