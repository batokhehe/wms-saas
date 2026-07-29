package service_test

import (
	"context"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
	"github.com/google/uuid"
	"strings"
	"sync"
)

type FakeManager struct{}

func (m *FakeManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakeRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.Product
	failOn map[string]error
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: map[uuid.UUID]*entity.Product{}, failOn: map[string]error{}}
}

func (r *fakeRepo) fail(m string, e error) { r.failOn[m] = e }

func (r *fakeRepo) Save(_ context.Context, p *entity.Product) error {
	if err := r.failOn["Save"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.byID {
		if existing.CompanyID() == p.CompanyID() && strings.EqualFold(string(existing.SKU()), string(p.SKU())) {
			return apperror.Conflict("duplicate SKU")
		}
	}
	r.byID[p.ID()] = p
	return nil
}

func (r *fakeRepo) Update(_ context.Context, p *entity.Product) error {
	if err := r.failOn["Update"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.byID[p.ID()]
	if !ok {
		return apperror.NotFound("not found")
	}
	if existing.Version() != p.Version() {
		return sharedrepo.ErrConcurrentModification
	}

	r.byID[p.ID()] = p
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, id, companyID uuid.UUID) (*entity.Product, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[id]
	if !ok || p.CompanyID() != companyID {
		return nil, apperror.NotFound("not found")
	}
	return p, nil
}

func (r *fakeRepo) List(_ context.Context, companyID uuid.UUID, filter repository.ListFilter) (pagination.Page[*entity.Product], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	matched := make([]*entity.Product, 0)
	for _, p := range r.byID {
		if p.CompanyID() == companyID && (filter.Status == "" || p.Status().String() == filter.Status) {
			matched = append(matched, p)
		}
	}
	return pagination.NewPage(matched, filter.Paging, int64(len(matched))), nil
}

func (r *fakeRepo) ExistsBySKU(_ context.Context, companyID uuid.UUID, sku entity.SKU) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.byID {
		if p.CompanyID() == companyID && strings.EqualFold(string(p.SKU()), string(sku)) {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepo) ExistsByBarcode(_ context.Context, companyID uuid.UUID, barcode string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.byID {
		if p.CompanyID() == companyID {
			for _, b := range p.Barcodes() {
				if strings.EqualFold(b.Code, barcode) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}
