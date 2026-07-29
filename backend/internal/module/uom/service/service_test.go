package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/batokhehe/wms-saas/backend/internal/module/uom/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/service"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/service/testdata/shared"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeIDGenerator struct{ id uuid.UUID }

func (g *fakeIDGenerator) NewID() uuid.UUID { return g.id }

func TestService_CreateUOM(t *testing.T) {
	repo := newFakeRepo()
	svc := service.New(repo, &fakeClock{time.Now()}, &fakeIDGenerator{uuid.New()}, &testutils.NoOpManager{})

	ctx := context.Background()
	req := dto.CreateUOMRequest{Code: "KG", Name: "Kilogram", Description: "Mass"}

	resp, err := svc.CreateUOM(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "KG", resp.Code)
}

func TestService_UpdateUOM(t *testing.T) {
	repo := newFakeRepo()
	id := uuid.New()
	uom, _ := entity.New(id, "KG", "Kilogram", "Mass", time.Now())
	repo.Save(context.Background(), uom)

	svc := service.New(repo, &fakeClock{time.Now()}, &fakeIDGenerator{id}, &testutils.NoOpManager{})

	ctx := context.Background()
	req := dto.UpdateUOMRequest{Name: "Kilogram Updated", Description: "New Mass"}

	resp, err := svc.UpdateUOM(ctx, id, req)
	require.NoError(t, err)
	assert.Equal(t, "Kilogram Updated", resp.Name)
}

func TestService_ActivateDeactivateUOM(t *testing.T) {
	repo := newFakeRepo()
	id := uuid.New()
	uom, _ := entity.New(id, "KG", "Kilogram", "Mass", time.Now())
	repo.Save(context.Background(), uom)

	svc := service.New(repo, &fakeClock{time.Now()}, &fakeIDGenerator{id}, &testutils.NoOpManager{})

	ctx := context.Background()

	// Deactivate
	resp, err := svc.DeactivateUOM(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, entity.StatusInactive.String(), string(resp.Status))

	// Activate
	resp, err = svc.ActivateUOM(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, entity.StatusActive.String(), string(resp.Status))
}

func TestService_GetUOMByID(t *testing.T) {
	repo := newFakeRepo()
	id := uuid.New()
	uom, _ := entity.New(id, "KG", "Kilogram", "Mass", time.Now())
	repo.Save(context.Background(), uom)

	svc := service.New(repo, &fakeClock{time.Now()}, &fakeIDGenerator{id}, &testutils.NoOpManager{})

	resp, err := svc.GetUOMByID(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, id, resp.ID)
}

func TestService_ListUOM(t *testing.T) {
	repo := newFakeRepo()
	uom1, _ := entity.New(uuid.New(), "KG", "Kilogram", "Mass", time.Now())
	repo.Save(context.Background(), uom1)

	svc := service.New(repo, &fakeClock{time.Now()}, &fakeIDGenerator{uuid.New()}, &testutils.NoOpManager{})

	page, err := svc.ListUOM(context.Background(), repository.ListFilter{})
	require.NoError(t, err)
	assert.Len(t, page.Items, 1)
}

func TestService_DuplicateCode(t *testing.T) {
	repo := newFakeRepo()
	uom1, _ := entity.New(uuid.New(), "KG", "Kilogram", "Mass", time.Now())
	repo.Save(context.Background(), uom1)

	svc := service.New(repo, &fakeClock{time.Now()}, &fakeIDGenerator{uuid.New()}, &testutils.NoOpManager{})

	_, err := svc.CreateUOM(context.Background(), dto.CreateUOMRequest{Code: "KG", Name: "Kilogram 2"})
	assert.Error(t, err)
	assert.ErrorIs(t, err, apperror.ErrConflict)
}

func TestService_OptimisticLocking(t *testing.T) {
	repo := newFakeRepo()
	id := uuid.New()
	uom, _ := entity.New(id, "KG", "Kilogram", "Mass", time.Now())
	repo.Save(context.Background(), uom)

	svc := service.New(repo, &fakeClock{time.Now()}, &fakeIDGenerator{id}, &testutils.NoOpManager{})

	// Simulate concurrent modification failure
	repo.fail("Update", sharedrepo.ErrConcurrentModification)

	// Try to update
	_, err := svc.UpdateUOM(context.Background(), id, dto.UpdateUOMRequest{Name: "Fail"})
	assert.Error(t, err)
	assert.ErrorIs(t, err, sharedrepo.ErrConcurrentModification)
}

func TestService_RepositoryError(t *testing.T) {
	repo := newFakeRepo()
	svc := service.New(repo, &fakeClock{time.Now()}, &fakeIDGenerator{uuid.New()}, &testutils.NoOpManager{})

	repo.fail("FindByID", apperror.Internal("db failure"))
	_, err := svc.GetUOMByID(context.Background(), uuid.New())
	assert.Error(t, err)
}
