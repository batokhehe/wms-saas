package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/location/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// ListFilter narrows a location listing.
//
// A plain struct rather than the module's DTO: the repository must not import
// dto, or the persistence layer would depend on the transport contract and a
// change to the API shape would ripple into SQL.
type ListFilter struct {
	Paging pagination.Request

	// WarehouseID scopes the listing to one site. Optional at this layer —
	// "every location in the company" is a legitimate query for a barcode
	// audit — but the service always supplies it for operational reads.
	WarehouseID uuid.UUID

	Status string
	Zone   string
}

// Repository is the persistence contract for the StorageLocation aggregate.
//
// # Two levels of scoping
//
// Every method takes a companyID, per RepositoryConvention §3. Methods that
// answer a question ABOUT A SITE also take a warehouseID, because the two are
// different boundaries: a company owns many warehouses, and a location code is
// unique within a warehouse rather than within the company.
//
// Making both required arguments means neither can be forgotten — the code
// simply will not compile.
//
// Note the signatures: they speak in *entity.StorageLocation and plain filters.
// No *gorm.DB, no Scope, no persistence model. GORM stops at this boundary, and
// so does locationModel, which is unexported.
type Repository interface {
	Save(ctx context.Context, l *entity.StorageLocation) error
	Update(ctx context.Context, l *entity.StorageLocation) error

	// SaveMany persists a batch of new locations.
	//
	// Rack layouts arrive as bulk imports — a single aisle is hundreds of bins —
	// and inserting them one round trip at a time is the difference between a
	// second and a minute.
	SaveMany(ctx context.Context, locations []*entity.StorageLocation) error

	// FindByID returns one location, or NOT_FOUND. A location belonging to
	// another company is NOT_FOUND, never FORBIDDEN — a 403 would confirm it
	// exists.
	FindByID(ctx context.Context, locationID, companyID uuid.UUID) (*entity.StorageLocation, error)

	// FindByCode resolves a location by its operator-facing code, which is
	// unique within a WAREHOUSE.
	FindByCode(
		ctx context.Context, companyID, warehouseID uuid.UUID, code string,
	) (*entity.StorageLocation, error)

	// FindByBarcode resolves a scanned label, which is unique within a COMPANY.
	//
	// Deliberately not warehouse-scoped: a scanner reads a barcode with no idea
	// which site it is standing in, and requiring the caller to supply one would
	// make the lookup impossible for the case it exists to serve.
	FindByBarcode(
		ctx context.Context, companyID uuid.UUID, barcode string,
	) (*entity.StorageLocation, error)

	List(
		ctx context.Context, companyID uuid.UUID, filter ListFilter,
	) (pagination.Page[*entity.StorageLocation], error)

	// ExistsByCode reports whether a code is taken within the warehouse.
	ExistsByCode(ctx context.Context, companyID, warehouseID uuid.UUID, code string) (bool, error)

	// ExistsByBarcode reports whether a barcode is taken within the company.
	ExistsByBarcode(ctx context.Context, companyID uuid.UUID, barcode string) (bool, error)

	// ExistsByBarcodeExcluding is the reassignment check: is this barcode held
	// by anyone OTHER than the location being relabelled? Without the exclusion,
	// re-assigning a location its own current barcode would report a conflict.
	ExistsByBarcodeExcluding(
		ctx context.Context, companyID uuid.UUID, barcode string, excludeID uuid.UUID,
	) (bool, error)

	// CountByWarehouse reports how many live locations a site has.
	CountByWarehouse(ctx context.Context, companyID, warehouseID uuid.UUID) (int64, error)
}

type locationRepository struct {
	repo *base.Base[locationModel, *locationModel]
}

var _ Repository = (*locationRepository)(nil)

// New builds the repository.
//
// The generic base is parameterised over the PERSISTENCE model, not the
// aggregate — the base requires entity.Identifiable, which locationModel gets
// from BaseEntity and the aggregate deliberately does not have.
//
// It is held as an unexported FIELD rather than embedded, so the base's CRUD is
// not promoted onto this type. Embedding would expose Create/Update/Delete
// operating on locationModel, letting a caller bypass the aggregate entirely.
func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &locationRepository{
		repo: base.New[locationModel, *locationModel](db, ids, "location.repository"),
	}
}

// forTenant is the company filter every method below applies.
//
// Wrapping it in a named helper means a reviewer auditing this file for missing
// tenant filters is looking for one identifier, not for an inline Where clause
// that is easy to skim past.
func forTenant(companyID uuid.UUID) base.Scope {
	return base.ForCompany(companyID)
}

// inWarehouse is the second, narrower scope.
func inWarehouse(warehouseID uuid.UUID) base.Scope {
	return base.Where("warehouse_id = ?", warehouseID)
}

func (r *locationRepository) Save(ctx context.Context, l *entity.StorageLocation) error {
	return r.repo.Create(ctx, toModel(l))
}

func (r *locationRepository) Update(ctx context.Context, l *entity.StorageLocation) error {
	return r.repo.UpdateOptimistic(ctx, toModel(l))
}

func (r *locationRepository) SaveMany(
	ctx context.Context, locations []*entity.StorageLocation,
) error {
	if len(locations) == 0 {
		return nil
	}

	models := make([]locationModel, 0, len(locations))
	for _, l := range locations {
		models = append(models, *toModel(l))
	}

	// Batched, because PostgreSQL caps a statement at 65535 bind parameters and
	// a rack layout can exceed that. The base picks a safe default size.
	return r.repo.CreateMany(ctx, models, 0)
}

func (r *locationRepository) FindByID(
	ctx context.Context, locationID, companyID uuid.UUID,
) (*entity.StorageLocation, error) {
	model, err := r.repo.FindByID(ctx, locationID, forTenant(companyID))
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *locationRepository) FindByCode(
	ctx context.Context, companyID, warehouseID uuid.UUID, code string,
) (*entity.StorageLocation, error) {
	model, err := r.repo.FindOne(ctx,
		forTenant(companyID),
		inWarehouse(warehouseID),
		base.Where("code = ?", normalizeCode(code)),
	)
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *locationRepository) FindByBarcode(
	ctx context.Context, companyID uuid.UUID, barcode string,
) (*entity.StorageLocation, error) {
	model, err := r.repo.FindOne(ctx,
		forTenant(companyID),
		// Compared exactly, not case-insensitively: a barcode is a machine
		// token reproduced by a scanner, and folding case would let two
		// physically distinct labels resolve to one location.
		base.Where("barcode = ?", strings.TrimSpace(barcode)),
	)
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *locationRepository) List(
	ctx context.Context, companyID uuid.UUID, filter ListFilter,
) (pagination.Page[*entity.StorageLocation], error) {
	scopes := []base.Scope{forTenant(companyID)}

	if filter.WarehouseID != uuid.Nil {
		scopes = append(scopes, inWarehouse(filter.WarehouseID))
	}
	if filter.Status != "" {
		scopes = append(scopes, base.Where("status = ?", filter.Status))
	}
	if filter.Zone != "" {
		scopes = append(scopes, base.Where("zone = ?", normalizeSegment(filter.Zone)))
	}
	if filter.Paging.HasSearch() {
		scopes = append(scopes, base.Search(filter.Paging.Search,
			"storage_locations.code", "storage_locations.barcode"))
	}

	// The base rejects a pagination.Request that has not been through Apply, so
	// an unvalidated sort column can never reach the SQL.
	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return pagination.Page[*entity.StorageLocation]{}, err
	}

	// Translated to aggregates before leaving the repository, so no caller ever
	// holds a persistence model.
	return pagination.Page[*entity.StorageLocation]{
		Items: toDomainSlice(page.Items),
		Meta:  page.Meta,
	}, nil
}

func (r *locationRepository) ExistsByCode(
	ctx context.Context, companyID, warehouseID uuid.UUID, code string,
) (bool, error) {
	return r.repo.ExistsBy(ctx,
		forTenant(companyID),
		inWarehouse(warehouseID),
		base.Where("code = ?", normalizeCode(code)),
	)
}

func (r *locationRepository) ExistsByBarcode(
	ctx context.Context, companyID uuid.UUID, barcode string,
) (bool, error) {
	return r.ExistsByBarcodeExcluding(ctx, companyID, barcode, uuid.Nil)
}

func (r *locationRepository) ExistsByBarcodeExcluding(
	ctx context.Context, companyID uuid.UUID, barcode string, excludeID uuid.UUID,
) (bool, error) {
	value := strings.TrimSpace(barcode)
	if value == "" {
		// An absent barcode never collides — the unique index is partial on
		// NOT NULL. Answering false here avoids a pointless query on every
		// create that omits one.
		return false, nil
	}

	scopes := []base.Scope{
		forTenant(companyID),
		base.Where("barcode = ?", value),
	}
	if excludeID != uuid.Nil {
		scopes = append(scopes, base.Where("id <> ?", excludeID))
	}

	return r.repo.ExistsBy(ctx, scopes...)
}

func (r *locationRepository) CountByWarehouse(
	ctx context.Context, companyID, warehouseID uuid.UUID,
) (int64, error) {
	return r.repo.Count(ctx, forTenant(companyID), inWarehouse(warehouseID))
}

// normalizeCode canonicalises a code for comparison.
//
// The column is CITEXT so comparison is already case-insensitive; normalising
// here keeps the query value consistent with what entity.NewLocationCode
// stores, so a lookup and an insert cannot disagree.
func normalizeCode(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

// normalizeSegment canonicalises a coordinate segment for filtering.
//
// Upper-cased because those columns are plain VARCHAR rather than CITEXT — see
// entity/coordinate.go for why.
func normalizeSegment(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}
