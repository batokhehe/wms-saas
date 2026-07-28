package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/infra/postgres"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
)

// translate funnels the child-table queries — the ones this repository issues
// directly rather than through the generic base — through the same error
// vocabulary the base uses, so a unique-barcode violation surfaces as CONFLICT
// rather than a raw driver error that would leak the constraint name.
func translate(err error, op string) error {
	return postgres.TranslateError(err, op)
}

// ListFilter narrows a product listing.
//
// A plain struct rather than the module's DTO: the repository must not import
// dto, or the persistence layer would depend on the transport contract.
type ListFilter struct {
	Paging   pagination.Request
	Status   string
	Tracking string
}

// Repository is the persistence contract for the Product aggregate.
//
// Every method takes a companyID. Products are tenant-owned, so
// RepositoryConvention §3 applies: making the tenant a required argument means
// forgetting it does not compile.
//
// The signatures speak in *entity.Product and plain filters — no *gorm.DB, no
// persistence model. GORM stops at this boundary, and so do the three
// unexported models.
//
// The Exists* methods are the queries the service's SPECIFICATIONS run
// (docs/Product.md §5): UniqueSKU, UniqueProductName and UniqueBarcode each
// resolve to one of them. They live here rather than in the service because
// only the repository can see the whole company's set.
type Repository interface {
	// Save persists a new aggregate and its child collections atomically.
	Save(ctx context.Context, p *entity.Product) error

	// Update persists changes to an existing aggregate under optimistic locking,
	// replacing its child collections in the same transaction.
	Update(ctx context.Context, p *entity.Product) error

	// FindByID returns one product with its children, or NOT_FOUND. A product
	// belonging to another company is NOT_FOUND, never FORBIDDEN — a 403 would
	// confirm it exists.
	FindByID(ctx context.Context, productID, companyID uuid.UUID) (*entity.Product, error)

	// FindBySKU resolves a product by its operator-facing SKU.
	FindBySKU(ctx context.Context, companyID uuid.UUID, sku string) (*entity.Product, error)

	List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.Product], error)

	// ExistsBySKU backs the UniqueSKU specification.
	ExistsBySKU(ctx context.Context, companyID uuid.UUID, sku string) (bool, error)

	// ExistsByName backs UniqueProductName at create time.
	ExistsByName(ctx context.Context, companyID uuid.UUID, name string) (bool, error)

	// ExistsByNameExcluding backs UniqueProductName on rename: is this name taken
	// by anyone OTHER than the product being renamed?
	ExistsByNameExcluding(ctx context.Context, companyID uuid.UUID, name string, excludeID uuid.UUID) (bool, error)

	// ExistsByBarcode backs the UniqueBarcode specification: is this barcode
	// already assigned to any product in the company?
	ExistsByBarcode(ctx context.Context, companyID uuid.UUID, barcode string) (bool, error)

	// ExistsByBarcodeExcluding is the same check while ignoring one product's own
	// rows, so re-persisting a product does not collide with itself.
	ExistsByBarcodeExcluding(ctx context.Context, companyID uuid.UUID, barcode string, excludeProductID uuid.UUID) (bool, error)
}

type productRepository struct {
	repo *base.Base[productModel, *productModel]
	db   *gorm.DB
	ids  port.IDGenerator
	tx   transaction.Manager
}

var _ Repository = (*productRepository)(nil)

// New builds the repository.
//
// It holds a transaction.Manager of its own so Save and Update can wrap their
// parent-plus-children writes atomically even when a caller invokes them outside
// a service transaction. When a caller (the service) is already in one, the
// manager joins it via a SAVEPOINT rather than opening a second — so the writes
// are one unit either way. See docs/Product.md §4.
func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &productRepository{
		repo: base.New[productModel, *productModel](db, ids, "product.repository"),
		db:   db,
		ids:  ids,
		tx:   transaction.NewGormManager(db),
	}
}

// forTenant is the scope every method below applies. A named helper means a
// reviewer auditing for missing tenant filters looks for one identifier.
func forTenant(companyID uuid.UUID) base.Scope {
	return base.ForCompany(companyID)
}

// Save writes the parent row and both child collections in one transaction.
func (r *productRepository) Save(ctx context.Context, p *entity.Product) error {
	return r.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := r.repo.Create(ctx, toModel(p)); err != nil {
			return err
		}
		return r.insertChildren(ctx, p)
	})
}

// Update persists the parent under optimistic locking, then replaces the child
// collections. The version check happens FIRST: if another writer won, the
// method returns the concurrency sentinel before any child row is touched, so a
// losing write never partially mutates the children.
func (r *productRepository) Update(ctx context.Context, p *entity.Product) error {
	return r.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := r.repo.UpdateOptimistic(ctx, toModel(p)); err != nil {
			return err
		}
		if err := r.deleteChildren(ctx, p.ID()); err != nil {
			return err
		}
		return r.insertChildren(ctx, p)
	})
}

// insertChildren assigns identifiers and writes the barcode and UOM rows.
//
// The aggregate is the source of truth for the whole set, so this is a plain
// insert of everything it currently holds — the delete in Update having already
// cleared the previous set. Empty collections are skipped: a product may have no
// barcodes, and GORM rejects an empty batch.
func (r *productRepository) insertChildren(ctx context.Context, p *entity.Product) error {
	barcodes := toBarcodeModels(p)
	for i := range barcodes {
		barcodes[i].ID = r.ids.NewID()
	}
	if len(barcodes) > 0 {
		if err := r.repo.DB(ctx).Create(&barcodes).Error; err != nil {
			return translate(err, "product.repository.insertBarcodes")
		}
	}

	uoms := toUOMModels(p)
	for i := range uoms {
		uoms[i].ID = r.ids.NewID()
	}
	if len(uoms) > 0 {
		if err := r.repo.DB(ctx).Create(&uoms).Error; err != nil {
			return translate(err, "product.repository.insertUOMs")
		}
	}
	return nil
}

// deleteChildren hard-deletes a product's child rows.
//
// Hard, not soft: child rows carry no soft-delete column, because the aggregate
// and its events are the audit trail. The replace-on-update strategy keeps the
// tables holding exactly the current set rather than an accumulating history.
func (r *productRepository) deleteChildren(ctx context.Context, productID uuid.UUID) error {
	if err := r.repo.DB(ctx).
		Where("product_id = ?", productID).
		Delete(&productBarcodeModel{}).Error; err != nil {
		return translate(err, "product.repository.deleteBarcodes")
	}
	if err := r.repo.DB(ctx).
		Where("product_id = ?", productID).
		Delete(&productUOMModel{}).Error; err != nil {
		return translate(err, "product.repository.deleteUOMs")
	}
	return nil
}

// FindByID loads the parent row, then its children, then reconstitutes.
func (r *productRepository) FindByID(
	ctx context.Context, productID, companyID uuid.UUID,
) (*entity.Product, error) {
	model, err := r.repo.FindByID(ctx, productID, forTenant(companyID))
	if err != nil {
		return nil, err
	}
	return r.hydrate(ctx, model)
}

// FindBySKU resolves a product by SKU within the company.
func (r *productRepository) FindBySKU(
	ctx context.Context, companyID uuid.UUID, sku string,
) (*entity.Product, error) {
	model, err := r.repo.FindOne(ctx,
		forTenant(companyID),
		base.Where("sku = ?", normalizeSKU(sku)),
	)
	if err != nil {
		return nil, err
	}
	return r.hydrate(ctx, model)
}

// hydrate loads one parent model's children and reconstitutes the aggregate.
func (r *productRepository) hydrate(ctx context.Context, model *productModel) (*entity.Product, error) {
	barcodes, uoms, err := r.loadChildren(ctx, []uuid.UUID{model.ID})
	if err != nil {
		return nil, err
	}
	return toDomain(model, barcodes[model.ID], uoms[model.ID]), nil
}

// List returns a page of the company's products, each with its children.
func (r *productRepository) List(
	ctx context.Context, companyID uuid.UUID, filter ListFilter,
) (pagination.Page[*entity.Product], error) {
	scopes := []base.Scope{forTenant(companyID)}

	if filter.Status != "" {
		scopes = append(scopes, base.Where("status = ?", filter.Status))
	}
	if filter.Tracking != "" {
		scopes = append(scopes, base.Where("tracking = ?", filter.Tracking))
	}
	if filter.Paging.HasSearch() {
		scopes = append(scopes, base.Search(filter.Paging.Search,
			"products.sku", "products.name"))
	}

	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return pagination.Page[*entity.Product]{}, err
	}

	// Batch-load the children for the whole page in two queries, not two per
	// row. Loading them lazily inside the loop below would be the N+1 problem
	// that makes a list endpoint fast in development and unusable in production.
	ids := make([]uuid.UUID, 0, len(page.Items))
	for i := range page.Items {
		ids = append(ids, page.Items[i].ID)
	}
	barcodes, uoms, err := r.loadChildren(ctx, ids)
	if err != nil {
		return pagination.Page[*entity.Product]{}, err
	}

	items := make([]*entity.Product, 0, len(page.Items))
	for i := range page.Items {
		model := &page.Items[i]
		items = append(items, toDomain(model, barcodes[model.ID], uoms[model.ID]))
	}

	return pagination.Page[*entity.Product]{Items: items, Meta: page.Meta}, nil
}

// loadChildren fetches the barcode and UOM rows for a set of products, grouped
// by product id. It is the single query pair that keeps List and FindByID free
// of N+1 loading.
func (r *productRepository) loadChildren(
	ctx context.Context, productIDs []uuid.UUID,
) (map[uuid.UUID][]productBarcodeModel, map[uuid.UUID][]productUOMModel, error) {
	barcodesByProduct := map[uuid.UUID][]productBarcodeModel{}
	uomsByProduct := map[uuid.UUID][]productUOMModel{}
	if len(productIDs) == 0 {
		return barcodesByProduct, uomsByProduct, nil
	}

	var barcodeRows []productBarcodeModel
	if err := r.repo.DB(ctx).
		Where("product_id IN ?", productIDs).
		Find(&barcodeRows).Error; err != nil {
		return nil, nil, translate(err, "product.repository.loadBarcodes")
	}
	for _, row := range barcodeRows {
		barcodesByProduct[row.ProductID] = append(barcodesByProduct[row.ProductID], row)
	}

	var uomRows []productUOMModel
	if err := r.repo.DB(ctx).
		Where("product_id IN ?", productIDs).
		Find(&uomRows).Error; err != nil {
		return nil, nil, translate(err, "product.repository.loadUOMs")
	}
	for _, row := range uomRows {
		uomsByProduct[row.ProductID] = append(uomsByProduct[row.ProductID], row)
	}

	return barcodesByProduct, uomsByProduct, nil
}

// ---------- specification queries ----------

func (r *productRepository) ExistsBySKU(
	ctx context.Context, companyID uuid.UUID, sku string,
) (bool, error) {
	return r.repo.ExistsBy(ctx,
		forTenant(companyID),
		base.Where("sku = ?", normalizeSKU(sku)),
	)
}

func (r *productRepository) ExistsByName(
	ctx context.Context, companyID uuid.UUID, name string,
) (bool, error) {
	return r.repo.ExistsBy(ctx,
		forTenant(companyID),
		base.Where("name = ?", normalizeName(name)),
	)
}

func (r *productRepository) ExistsByNameExcluding(
	ctx context.Context, companyID uuid.UUID, name string, excludeID uuid.UUID,
) (bool, error) {
	return r.repo.ExistsBy(ctx,
		forTenant(companyID),
		base.Where("name = ?", normalizeName(name)),
		base.Where("id <> ?", excludeID),
	)
}

// ExistsByBarcode queries the child table directly, because a barcode lives on
// product_barcodes rather than on products. The tenant filter is on the child
// row's own company_id, which is why that column is denormalised onto it.
func (r *productRepository) ExistsByBarcode(
	ctx context.Context, companyID uuid.UUID, barcode string,
) (bool, error) {
	return r.barcodeExists(ctx, companyID, barcode, uuid.Nil)
}

func (r *productRepository) ExistsByBarcodeExcluding(
	ctx context.Context, companyID uuid.UUID, barcode string, excludeProductID uuid.UUID,
) (bool, error) {
	return r.barcodeExists(ctx, companyID, barcode, excludeProductID)
}

func (r *productRepository) barcodeExists(
	ctx context.Context, companyID uuid.UUID, barcode string, excludeProductID uuid.UUID,
) (bool, error) {
	query := r.repo.DB(ctx).
		Model(&productBarcodeModel{}).
		Where("company_id = ? AND barcode = ?", companyID, normalizeBarcode(barcode))
	if excludeProductID != uuid.Nil {
		query = query.Where("product_id <> ?", excludeProductID)
	}

	var found int64
	if err := query.Select("1").Limit(1).Scan(&found).Error; err != nil {
		return false, translate(err, "product.repository.barcodeExists")
	}
	return found == 1, nil
}

// ---------- normalisation ----------
//
// The columns are CITEXT so comparison is already case-insensitive; normalising
// the query value keeps it consistent with what the value objects store, so a
// lookup and an insert cannot disagree.

func normalizeSKU(raw string) string     { return strings.ToUpper(strings.TrimSpace(raw)) }
func normalizeName(raw string) string    { return strings.TrimSpace(raw) }
func normalizeBarcode(raw string) string { return strings.TrimSpace(raw) }
