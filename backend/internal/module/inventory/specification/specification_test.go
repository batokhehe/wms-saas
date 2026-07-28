package specification

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// fakeRepo is an in-memory stand-in for repository.Repository. It is NOT a
// persistence test — it exists only so the set-level specifications can be
// exercised without infrastructure.
type fakeRepo struct {
	items []*entity.Inventory
}

var _ repository.Repository = (*fakeRepo)(nil)

func (r *fakeRepo) Save(_ context.Context, inv *entity.Inventory) error {
	r.items = append(r.items, inv)
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, id, companyID uuid.UUID) (*entity.Inventory, error) {
	for _, inv := range r.items {
		if inv.ID() == id && inv.CompanyID() == companyID {
			return inv, nil
		}
	}
	return nil, apperror.NotFound("not found")
}

func (r *fakeRepo) FindByProductLocation(_ context.Context, companyID, productID, locationID uuid.UUID) ([]*entity.Inventory, error) {
	var out []*entity.Inventory
	for _, inv := range r.items {
		if inv.CompanyID() == companyID && inv.ProductID() == productID && inv.LocationID() == locationID {
			out = append(out, inv)
		}
	}
	return out, nil
}

func (r *fakeRepo) FindByLot(_ context.Context, companyID, productID, locationID uuid.UUID, lot string) (*entity.Inventory, error) {
	for _, inv := range r.items {
		if inv.CompanyID() == companyID && inv.ProductID() == productID &&
			inv.LocationID() == locationID && inv.Lot().String() == lot {
			return inv, nil
		}
	}
	return nil, apperror.NotFound("not found")
}

func (r *fakeRepo) FindBySerial(_ context.Context, companyID, productID uuid.UUID, serial string) (*entity.Inventory, error) {
	for _, inv := range r.items {
		if inv.CompanyID() == companyID && inv.ProductID() == productID && inv.Serial().String() == serial {
			return inv, nil
		}
	}
	return nil, apperror.NotFound("not found")
}

func (r *fakeRepo) List(_ context.Context, _ uuid.UUID, _ repository.ListFilter) (pagination.Page[*entity.Inventory], error) {
	return pagination.Page[*entity.Inventory]{}, nil
}

func (r *fakeRepo) Exists(_ context.Context, companyID, productID, locationID uuid.UUID) (bool, error) {
	items, _ := r.FindByProductLocation(context.Background(), companyID, productID, locationID)
	return len(items) > 0, nil
}

func mkNone(company, product, location uuid.UUID, onHand int64) *entity.Inventory {
	inv, err := entity.NewInventory(uuid.New(), company, uuid.New(), location, product,
		entity.TrackingNone, entity.NoLotNumber(), entity.NoSerialNumber(),
		entity.MustInventoryQuantity(onHand), uuid.New(), time.Now().UTC())
	if err != nil {
		panic(err)
	}
	return inv
}

func TestInventoryExists(t *testing.T) {
	company, product, location := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeRepo{}
	spec := NewInventoryExists(repo)

	ok, err := spec.Holds(context.Background(), company, product, location)
	if err != nil || ok {
		t.Fatalf("expected no inventory yet: ok=%v err=%v", ok, err)
	}
	_ = repo.Save(context.Background(), mkNone(company, product, location, 5))
	ok, err = spec.Holds(context.Background(), company, product, location)
	if err != nil || !ok {
		t.Fatalf("expected inventory to exist: ok=%v err=%v", ok, err)
	}
}

func TestEnoughAvailableInventory(t *testing.T) {
	inv := mkNone(uuid.New(), uuid.New(), uuid.New(), 10)
	spec := NewEnoughAvailableInventory()

	if !spec.Holds(inv, entity.MustQuantity(10)) {
		t.Error("10 available should satisfy a request for 10")
	}
	if spec.Holds(inv, entity.MustQuantity(11)) {
		t.Error("10 available should not satisfy a request for 11")
	}
	if err := spec.Ensure(inv, entity.MustQuantity(11)); err == nil {
		t.Error("Ensure should reject an over-request")
	}
	if code := apperror.From(spec.Ensure(inv, entity.MustQuantity(11))).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
	if err := spec.Ensure(inv, entity.MustQuantity(10)); err != nil {
		t.Errorf("Ensure(10) = %v, want nil", err)
	}
}

func TestUniqueSerial(t *testing.T) {
	company, product := uuid.New(), uuid.New()
	repo := &fakeRepo{}
	spec := NewUniqueSerial(repo)

	if err := spec.Ensure(context.Background(), company, product, "SN-1"); err != nil {
		t.Fatalf("a fresh serial should be unique: %v", err)
	}
	serial, _ := entity.NewSerialNumber("SN-1")
	inv, _ := entity.NewInventory(uuid.New(), company, uuid.New(), uuid.New(), product,
		entity.TrackingSerial, entity.NoLotNumber(), serial, entity.MustInventoryQuantity(1), uuid.New(), time.Now().UTC())
	_ = repo.Save(context.Background(), inv)

	err := spec.Ensure(context.Background(), company, product, "SN-1")
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT for a duplicate serial", code)
	}
}

func TestUniqueLot(t *testing.T) {
	company, product, location := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeRepo{}
	spec := NewUniqueLot(repo)

	if err := spec.Ensure(context.Background(), company, product, location, "LOT-A"); err != nil {
		t.Fatalf("a fresh lot should be unique: %v", err)
	}
	lot, _ := entity.NewLotNumber("LOT-A")
	inv, _ := entity.NewInventory(uuid.New(), company, uuid.New(), location, product,
		entity.TrackingLot, lot, entity.NoSerialNumber(), entity.MustInventoryQuantity(50), uuid.New(), time.Now().UTC())
	_ = repo.Save(context.Background(), inv)

	err := spec.Ensure(context.Background(), company, product, location, "LOT-A")
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT for a duplicate lot", code)
	}
}
