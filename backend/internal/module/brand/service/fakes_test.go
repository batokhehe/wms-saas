package service_test

import (
	"context"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
	"github.com/google/uuid"
	"strings"
	"sync"
)

type fakeRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.Brand
	failOn map[string]error
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: map[uuid.UUID]*entity.Brand{}, failOn: map[string]error{}}
}

func (r *fakeRepo) fail(m string, e error) { r.failOn[m] = e }

func (r *fakeRepo) Save(_ context.Context, b *entity.Brand) error {
	if err := r.failOn["Save"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.byID {
		if existing.CompanyID() == b.CompanyID() && strings.EqualFold(existing.Code(), b.Code()) {
			return apperror.Conflict("duplicate code")
		}
	}
	r.byID[b.ID()] = b
	return nil
}

func (r *fakeRepo) Update(_ context.Context, b *entity.Brand) error {
	if err := r.failOn["Update"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.byID[b.ID()]
	if !ok {
		return apperror.NotFound("not found")
	}
	if existing.Version() != b.Version() {
		return sharedrepo.ErrConcurrentModification
	}
	r.byID[b.ID()], _ = entity.ReconstituteWithVersion(
		b.ID(), b.CompanyID(), b.Code(), b.Name(), b.Description(), b.Status(),
		b.CreatedAt(), b.UpdatedAt(), b.DeletedAt(), b.Version()+1,
	)
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, id, companyID uuid.UUID) (*entity.Brand, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.byID[id]
	if !ok || b.CompanyID() != companyID {
		return nil, apperror.NotFound("not found")
	}
	return b, nil
}

func (r *fakeRepo) List(_ context.Context, companyID uuid.UUID, filter repository.ListFilter) (pagination.Page[*entity.Brand], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	matched := make([]*entity.Brand, 0)
	for _, b := range r.byID {
		if b.CompanyID() == companyID && (filter.Status == "" || b.Status().String() == filter.Status) {
			matched = append(matched, b)
		}
	}
	return pagination.NewPage(matched, filter.Paging, int64(len(matched))), nil
}

func (r *fakeRepo) ExistsByCode(_ context.Context, companyID uuid.UUID, code string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.byID {
		if b.CompanyID() == companyID && strings.EqualFold(b.Code(), code) {
			return true, nil
		}
	}
	return false, nil
}
