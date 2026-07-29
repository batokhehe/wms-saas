package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
	"go.uber.org/zap"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeIDGenerator struct{ id uuid.UUID }

func (g *fakeIDGenerator) NewID() uuid.UUID { return g.id }

type mockVerifier struct {
	exists bool
	err    error
}

func (m *mockVerifier) Exists(_ context.Context, _ uuid.UUID) (bool, error) { return m.exists, m.err }
func (m *mockVerifier) HasStock(_ uuid.UUID) (bool, error)                  { return m.exists, m.err }
func (m *mockVerifier) HasMovements(_ uuid.UUID) (bool, error)              { return m.exists, m.err }

func ctxWithActor(tID, uID uuid.UUID) context.Context {
	rc := appcontext.New("test-req-id", zap.NewNop())
	rc.WithCompany(tID, uuid.New(), "admin")
	rc.WithTenant(&tID, &uID, "admin")
	return appcontext.Into(context.Background(), rc)
}

func TestService(t *testing.T) {
	repo := newFakeRepo()
	v := &mockVerifier{exists: true}
	svc := service.New(repo, &fakeClock{time.Now()}, &fakeIDGenerator{uuid.New()}, &FakeManager{}, v, v, v, v, v)
	tID, uID := uuid.New(), uuid.New()
	ctx := ctxWithActor(tID, uID)

	t.Run("Create", func(t *testing.T) {
		req := dto.CreateProductRequest{SKU: "SKU1", Name: "P1", CategoryID: uuid.New(), BrandID: uuid.New(), BaseUOMID: uuid.New()}
		res, err := svc.CreateProduct(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, "SKU1", res.SKU)
	})

	t.Run("DuplicateSKU", func(t *testing.T) {
		_, err := svc.CreateProduct(ctx, dto.CreateProductRequest{SKU: "SKU1", Name: "P2", CategoryID: uuid.New(), BrandID: uuid.New(), BaseUOMID: uuid.New()})
		assert.ErrorIs(t, err, apperror.ErrConflict)
	})

	t.Run("Archive", func(t *testing.T) {
		p, _ := entity.New(uuid.New(), tID, uuid.New(), uuid.New(), uuid.New(), entity.SKU("SKU2"), entity.ProductName("Name"), "", entity.TrackingConfig{}, entity.InventoryPolicy{}, entity.Dimensions{}, entity.Weight{}, entity.Volume{}, time.Now())
		repo.Save(ctx, p)

		// Use a verifier that says no stock
		svcNoStock := service.New(repo, &fakeClock{time.Now()}, &fakeIDGenerator{uuid.New()}, &FakeManager{}, v, v, v, &mockVerifier{exists: false}, v)
		err := svcNoStock.ArchiveProduct(ctx, p.ID())
		require.NoError(t, err)
		updated, _ := repo.FindByID(ctx, p.ID(), tID)
		assert.True(t, updated.IsArchived())
	})

	t.Run("Update", func(t *testing.T) {
		p, _ := entity.New(uuid.New(), tID, uuid.New(), uuid.New(), uuid.New(), entity.SKU("SKU3"), entity.ProductName("Name"), "", entity.TrackingConfig{}, entity.InventoryPolicy{}, entity.Dimensions{}, entity.Weight{}, entity.Volume{}, time.Now())
		repo.Save(ctx, p)
		res, err := svc.UpdateProduct(ctx, p.ID(), dto.UpdateProductRequest{Name: "New Name"})
		require.NoError(t, err)
		assert.Equal(t, "New Name", res.Name)
	})

	t.Run("ActivateDeactivate", func(t *testing.T) {
		p, _ := entity.New(uuid.New(), tID, uuid.New(), uuid.New(), uuid.New(), entity.SKU("SKU4"), entity.ProductName("Name"), "", entity.TrackingConfig{}, entity.InventoryPolicy{}, entity.Dimensions{}, entity.Weight{}, entity.Volume{}, time.Now())
		repo.Save(ctx, p)
		res, err := svc.DeactivateProduct(ctx, p.ID())
		require.NoError(t, err)
		assert.Equal(t, "INACTIVE", res.Status)
		res, err = svc.ActivateProduct(ctx, p.ID())
		require.NoError(t, err)
		assert.Equal(t, "ACTIVE", res.Status)
	})

	t.Run("GetByID", func(t *testing.T) {
		p, _ := entity.New(uuid.New(), tID, uuid.New(), uuid.New(), uuid.New(), entity.SKU("SKU5"), entity.ProductName("Name"), "", entity.TrackingConfig{}, entity.InventoryPolicy{}, entity.Dimensions{}, entity.Weight{}, entity.Volume{}, time.Now())
		repo.Save(ctx, p)
		res, err := svc.GetProductByID(ctx, p.ID())
		require.NoError(t, err)
		assert.Equal(t, p.ID(), res.ID)
	})

	t.Run("List", func(t *testing.T) {
		page, err := svc.ListProducts(ctx, repository.ListFilter{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(page.Items), 1)
	})

	t.Run("OptimisticLocking", func(t *testing.T) {
		p, _ := entity.New(uuid.New(), tID, uuid.New(), uuid.New(), uuid.New(), entity.SKU("SKU6"), entity.ProductName("Name"), "", entity.TrackingConfig{}, entity.InventoryPolicy{}, entity.Dimensions{}, entity.Weight{}, entity.Volume{}, time.Now())
		repo.Save(ctx, p)
		repo.fail("Update", sharedrepo.ErrConcurrentModification)
		_, err := svc.ActivateProduct(ctx, p.ID())
		assert.ErrorIs(t, err, sharedrepo.ErrConcurrentModification)
	})

	t.Run("RepositoryError", func(t *testing.T) {
		repo.fail("FindByID", apperror.Internal("db failure"))
		_, err := svc.GetProductByID(ctx, uuid.New())
		assert.Error(t, err)
	})
}
