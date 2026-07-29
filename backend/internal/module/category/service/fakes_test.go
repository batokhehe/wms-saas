package service_test

import (
	"context"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
	"github.com/google/uuid"
	"strings"
	"sync"
)

type fakeRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.Category
	failOn map[string]error
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: map[uuid.UUID]*entity.Category{}, failOn: map[string]error{}}
}

func (r *fakeRepo) fail(m string, e error) { r.failOn[m] = e }

func (r *fakeRepo) Save(_ context.Context, c *entity.Category) error {
	if err := r.failOn["Save"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.byID {
		if existing.CompanyID() == c.CompanyID() && strings.EqualFold(existing.Code(), c.Code()) {
			return apperror.Conflict("duplicate code")
		}
	}
	r.byID[c.ID()] = c
	return nil
}

func (r *fakeRepo) Update(_ context.Context, c *entity.Category) error {
	if err := r.failOn["Update"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.byID[c.ID()]
	if !ok {
		return apperror.NotFound("not found")
	}
	if existing.Version() != c.Version() {
		return sharedrepo.ErrConcurrentModification
	}
	r.byID[c.ID()], _ = entity.ReconstituteWithVersion(
		c.ID(), c.CompanyID(), c.Code(), c.Name(), c.Description(), c.Status(),
		c.CreatedAt(), c.UpdatedAt(), c.DeletedAt(), c.Version()+1,
	)
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, id, companyID uuid.UUID) (*entity.Category, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.byID[id]
	if !ok || c.CompanyID() != companyID {
		return nil, apperror.NotFound("not found")
	}
	return c, nil
}

func (r *fakeRepo) List(_ context.Context, companyID uuid.UUID, filter repository.ListFilter) (pagination.Page[*entity.Category], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	matched := make([]*entity.Category, 0)
	for _, c := range r.byID {
		if c.CompanyID() == companyID && (filter.Status == "" || c.Status().String() == filter.Status) {
			matched = append(matched, c)
		}
	}
	return pagination.NewPage(matched, filter.Paging, int64(len(matched))), nil
}

func (r *fakeRepo) ExistsByCode(_ context.Context, companyID uuid.UUID, code string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.byID {
		if c.CompanyID() == companyID && strings.EqualFold(c.Code(), code) {
			return true, nil
		}
	}
	return false, nil
}
