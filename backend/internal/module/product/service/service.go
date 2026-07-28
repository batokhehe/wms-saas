package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Service orchestrates the Product aggregate.
//
// Read every method below and notice what is absent: there is no `if status ==`,
// no readiness check, no state machine, no "exactly one primary barcode" logic.
// Those live in the aggregate. What is here is the shape every method shares:
//
//	resolve tenant and actor → load aggregate → gather cross-aggregate facts
//	  (verifiers, inventory) → check set-level rules (specifications) → call ONE
//	  domain method → persist → publish
//
// The set-level checks (is this SKU taken?) are SPECIFICATIONS; the
// cross-aggregate facts (does this category exist? is there stock?) come from
// VERIFIERS and the InventoryProvider. Neither can live in the aggregate: one
// needs the whole set, the other needs another aggregate.
type Service struct {
	repo       repository.Repository
	specs      Specifications
	categories CategoryVerifier
	brands     BrandVerifier
	uoms       UOMVerifier
	inventory  InventoryProvider
	clock      port.Clock
	ids        port.IDGenerator
	tx         transaction.Manager
	events     EventPublisher
}

// New builds the service.
func New(
	repo repository.Repository,
	categories CategoryVerifier,
	brands BrandVerifier,
	uoms UOMVerifier,
	inventory InventoryProvider,
	clock port.Clock,
	ids port.IDGenerator,
	tx transaction.Manager,
	events EventPublisher,
) *Service {
	return &Service{
		repo:       repo,
		specs:      NewSpecifications(repo),
		categories: categories,
		brands:     brands,
		uoms:       uoms,
		inventory:  inventory,
		clock:      clock,
		ids:        ids,
		tx:         tx,
		events:     events,
	}
}

// actor resolves the tenant and the acting user together. Every method starts
// here. Both come from the RequestContext, never from a request field, so a
// client cannot name a company or impersonate a user.
func (s *Service) actor(ctx context.Context) (companyID, userID uuid.UUID, err error) {
	rc := appcontext.From(ctx)
	if userID, err = rc.RequireUser(); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if companyID, err = rc.RequireTenant(); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return companyID, userID, nil
}

// Create registers a product in DRAFT.
func (s *Service) Create(
	ctx context.Context, req dto.CreateProductRequest,
) (dto.ProductResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.ProductResponse{}, err
	}

	// The value objects validate themselves; the service never inspects the raw
	// strings.
	sku, err := entity.NewSKU(req.SKU)
	if err != nil {
		return dto.ProductResponse{}, err
	}
	name, err := entity.NewProductName(req.Name)
	if err != nil {
		return dto.ProductResponse{}, err
	}
	if req.BaseUOMID == uuid.Nil {
		return dto.ProductResponse{}, apperror.Validation("base unit of measure is required")
	}

	var response dto.ProductResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		// The base unit must be a real unit — a product whose base unit does not
		// exist has every quantity expressed in nothing.
		if err := s.uoms.VerifyUOM(ctx, companyID, req.BaseUOMID); err != nil {
			return err
		}

		// Specifications: only the repository can see siblings. Checked
		// explicitly so the common case gets a clear message; the unique indexes
		// remain the real guarantee against a race.
		if err := s.specs.UniqueSKU.Satisfy(ctx, companyID, sku.String()); err != nil {
			return err
		}
		if err := s.specs.UniqueProductName.Satisfy(ctx, companyID, name.String(), uuid.Nil); err != nil {
			return err
		}

		// The FACTORY builds the aggregate — always DRAFT, tracking NONE, base
		// unit provisioned with factor 1, raising ProductCreated. The service
		// cannot construct one any other way.
		product, err := entity.NewProduct(
			s.ids.NewID(), companyID, sku, name, req.Description, req.BaseUOMID,
			actorID, s.clock.Now(),
		)
		if err != nil {
			return err
		}

		if err := s.repo.Save(ctx, product); err != nil {
			return err
		}

		response = mapper.ToResponse(product)
		s.publish(ctx, product)
		return nil
	})
	if err != nil {
		return dto.ProductResponse{}, err
	}
	return response, nil
}

// Get returns one product.
func (s *Service) Get(ctx context.Context, productID uuid.UUID) (dto.ProductResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.ProductResponse{}, err
	}
	product, err := s.repo.FindByID(ctx, productID, companyID)
	if err != nil {
		return dto.ProductResponse{}, err
	}
	return mapper.ToResponse(product), nil
}

// List returns a page of the company's products.
func (s *Service) List(
	ctx context.Context, query dto.ListProductsQuery,
) (pagination.Page[dto.ProductResponse], error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return pagination.Page[dto.ProductResponse]{}, err
	}

	// Apply resolves the sort key against the endpoint's allow-list. It must run
	// before the query reaches the repository; the base repository refuses an
	// unapplied request.
	if err := query.Request.Apply(dto.SortOptions()); err != nil {
		return pagination.Page[dto.ProductResponse]{}, err
	}

	page, err := s.repo.List(ctx, companyID, repository.ListFilter{
		Paging:   query.Request,
		Status:   query.Status,
		Tracking: query.Tracking,
	})
	if err != nil {
		return pagination.Page[dto.ProductResponse]{}, err
	}
	return mapper.ToPage(page), nil
}

// Update applies a partial attribute update: name and description.
//
// Each supplied field becomes a separate CALL on the aggregate rather than a
// bulk assignment — the difference between a domain model and a record. Rename
// runs the UniqueProductName specification, excluding the product itself so
// renaming it to its own name is not a conflict.
func (s *Service) Update(
	ctx context.Context, productID uuid.UUID, req dto.UpdateProductRequest,
) (dto.ProductResponse, error) {
	return s.mutate(ctx, productID, "Update",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			if req.Name != nil {
				name, err := entity.NewProductName(*req.Name)
				if err != nil {
					return err
				}
				if err := s.specs.UniqueProductName.Satisfy(ctx, p.CompanyID(), name.String(), p.ID()); err != nil {
					return err
				}
				if err := p.Rename(name, actorID, now); err != nil {
					return err
				}
			}
			if req.Description != nil {
				p.ChangeDescription(*req.Description, actorID, now)
			}
			return nil
		})
}

// AssignCategory sets or clears the product's category.
//
// A non-nil id is verified to exist in the company before assignment; a nil id
// clears the category, which needs no verification.
func (s *Service) AssignCategory(
	ctx context.Context, productID uuid.UUID, req dto.AssignCategoryRequest,
) (dto.ProductResponse, error) {
	return s.mutate(ctx, productID, "AssignCategory",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			if req.CategoryID != nil {
				if err := s.categories.VerifyCategory(ctx, p.CompanyID(), *req.CategoryID); err != nil {
					return err
				}
			}
			return p.AssignCategory(req.CategoryID, actorID, now)
		})
}

// AssignBrand sets or clears the product's brand.
func (s *Service) AssignBrand(
	ctx context.Context, productID uuid.UUID, req dto.AssignBrandRequest,
) (dto.ProductResponse, error) {
	return s.mutate(ctx, productID, "AssignBrand",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			if req.BrandID != nil {
				if err := s.brands.VerifyBrand(ctx, p.CompanyID(), *req.BrandID); err != nil {
					return err
				}
			}
			return p.AssignBrand(req.BrandID, actorID, now)
		})
}

// SetMeasurements replaces the physical profile. Each measurement is built into
// its value object here; the aggregate validates and rejects a partial one.
func (s *Service) SetMeasurements(
	ctx context.Context, productID uuid.UUID, req dto.SetMeasurementsRequest,
) (dto.ProductResponse, error) {
	weight, dimension, volume, err := buildMeasurements(req)
	if err != nil {
		return dto.ProductResponse{}, err
	}
	return s.mutate(ctx, productID, "SetMeasurements",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			return p.SetMeasurements(weight, dimension, volume, actorID, now)
		})
}

// SetShelfLife sets or clears the shelf life. A nil Days clears it (undefined);
// a present value — including zero — defines it.
func (s *Service) SetShelfLife(
	ctx context.Context, productID uuid.UUID, req dto.SetShelfLifeRequest,
) (dto.ProductResponse, error) {
	shelfLife := entity.NoShelfLife()
	if req.Days != nil {
		built, err := entity.NewShelfLife(*req.Days)
		if err != nil {
			return dto.ProductResponse{}, err
		}
		shelfLife = built
	}
	return s.mutate(ctx, productID, "SetShelfLife",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			p.SetShelfLife(shelfLife, actorID, now)
			return nil
		})
}

// SetTracking changes the tracking method.
//
// The aggregate refuses the change while stock exists; whether stock exists is a
// fact from the Inventory aggregate, fetched here and passed IN. The rule stays
// in the domain.
func (s *Service) SetTracking(
	ctx context.Context, productID uuid.UUID, req dto.SetTrackingRequest,
) (dto.ProductResponse, error) {
	method := entity.TrackingMethod(req.Method)
	return s.mutate(ctx, productID, "SetTracking",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			hasInventory, err := s.inventory.HasInventory(ctx, p.CompanyID(), p.ID())
			if err != nil {
				return err
			}
			return p.SetTracking(method, hasInventory, actorID, now)
		})
}

// AddBarcode assigns a barcode. The UniqueBarcode specification runs first,
// excluding this product's own rows, so a cross-product collision is rejected
// with a clear message before the aggregate is touched.
func (s *Service) AddBarcode(
	ctx context.Context, productID uuid.UUID, req dto.AddBarcodeRequest,
) (dto.ProductResponse, error) {
	barcode, err := entity.NewBarcode(req.Barcode)
	if err != nil {
		return dto.ProductResponse{}, err
	}
	return s.mutate(ctx, productID, "AddBarcode",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			if err := s.specs.UniqueBarcode.Satisfy(ctx, p.CompanyID(), barcode.String(), p.ID()); err != nil {
				return err
			}
			return p.AddBarcode(barcode, req.Primary, actorID, now)
		})
}

// SetPrimaryBarcode promotes an existing barcode to primary.
func (s *Service) SetPrimaryBarcode(
	ctx context.Context, productID uuid.UUID, req dto.SetPrimaryBarcodeRequest,
) (dto.ProductResponse, error) {
	barcode, err := entity.NewBarcode(req.Barcode)
	if err != nil {
		return dto.ProductResponse{}, err
	}
	return s.mutate(ctx, productID, "SetPrimaryBarcode",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			return p.SetPrimaryBarcode(barcode, actorID, now)
		})
}

// RemoveBarcode unassigns a barcode.
func (s *Service) RemoveBarcode(
	ctx context.Context, productID uuid.UUID, rawBarcode string,
) (dto.ProductResponse, error) {
	barcode, err := entity.NewBarcode(rawBarcode)
	if err != nil {
		return dto.ProductResponse{}, err
	}
	return s.mutate(ctx, productID, "RemoveBarcode",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			return p.RemoveBarcode(barcode, actorID, now)
		})
}

// AddUOM adds an alternate unit of measure. The unit is verified to exist, and
// the conversion factor is built into its value object, which validates it is a
// positive exact decimal.
func (s *Service) AddUOM(
	ctx context.Context, productID uuid.UUID, req dto.AddUOMRequest,
) (dto.ProductResponse, error) {
	if req.UOMID == uuid.Nil {
		return dto.ProductResponse{}, apperror.Validation("unit of measure id is required")
	}
	factor, err := entity.NewConversionFactor(req.Factor)
	if err != nil {
		return dto.ProductResponse{}, err
	}
	return s.mutate(ctx, productID, "AddUOM",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			if err := s.uoms.VerifyUOM(ctx, p.CompanyID(), req.UOMID); err != nil {
				return err
			}
			return p.AddUOM(req.UOMID, factor, actorID, now)
		})
}

// RemoveUOM removes an alternate unit. The aggregate refuses to remove the base.
func (s *Service) RemoveUOM(
	ctx context.Context, productID, uomID uuid.UUID,
) (dto.ProductResponse, error) {
	return s.mutate(ctx, productID, "RemoveUOM",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			return p.RemoveUOM(uomID, actorID, now)
		})
}

// Activate makes a product available for operations.
func (s *Service) Activate(ctx context.Context, productID uuid.UUID) (dto.ProductResponse, error) {
	return s.mutate(ctx, productID, "Activate",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			return p.Activate(actorID, now)
		})
}

// Deactivate returns a product to DRAFT.
func (s *Service) Deactivate(ctx context.Context, productID uuid.UUID) (dto.ProductResponse, error) {
	return s.mutate(ctx, productID, "Deactivate",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			return p.Deactivate(actorID, now)
		})
}

// Discontinue permanently retires a product. DISCONTINUED is terminal.
func (s *Service) Discontinue(ctx context.Context, productID uuid.UUID) (dto.ProductResponse, error) {
	return s.mutate(ctx, productID, "Discontinue",
		func(ctx context.Context, p *entity.Product, actorID uuid.UUID, now time.Time) error {
			return p.Discontinue(actorID, now)
		})
}

// mutate is the shared shape of every load-modify-persist operation.
//
// Load, run the closure (which may gather facts and call one or more domain
// methods), persist, publish — all inside one transaction. Factoring it out
// means the transaction boundary, the tenant defence check and the event
// publication are identical for every operation rather than re-derived a dozen
// times, and a new operation cannot forget any of them.
//
// The read-modify-write MUST be atomic: without the transaction, two concurrent
// updates both read the same row and the second silently overwrites the first.
func (s *Service) mutate(
	ctx context.Context,
	productID uuid.UUID,
	op string,
	apply func(context.Context, *entity.Product, uuid.UUID, time.Time) error,
) (dto.ProductResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.ProductResponse{}, err
	}

	var response dto.ProductResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		product, err := s.repo.FindByID(ctx, productID, companyID)
		if err != nil {
			return err
		}

		// Defence in depth. The repository already filtered by tenant, so this
		// can only fire if that filter is ever broken — which is exactly when a
		// cross-tenant write would otherwise go unnoticed.
		if product.CompanyID() != companyID {
			return apperror.Forbidden("This product belongs to another company").
				WithOp("product.service." + op)
		}

		if err := apply(ctx, product, actorID, s.clock.Now()); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, product); err != nil {
			return err
		}

		response = mapper.ToResponse(product)
		s.publish(ctx, product)
		return nil
	})
	if err != nil {
		return dto.ProductResponse{}, s.concurrentModification(ctx, productID, companyID, err)
	}
	return response, nil
}

// concurrentModification turns the repository sentinel into the API's normal 409
// after the transaction has rolled back. A fresh read returns the winning
// writer's version, which is the token a client needs before retrying.
func (s *Service) concurrentModification(ctx context.Context, id, companyID uuid.UUID, err error) error {
	if err == nil || !errors.Is(err, sharedrepo.ErrConcurrentModification) {
		return err
	}
	current, readErr := s.repo.FindByID(ctx, id, companyID)
	if readErr != nil {
		return err
	}
	return apperror.Conflict("The resource was changed by another request").
		WithOp("product.service.concurrentModification").
		WithDetails(map[string]any{"entity_id": id, "current_version": current.Version()}).
		WithCause(sharedrepo.ErrConcurrentModification)
}

// publish drains the aggregate's recorded events and publishes them.
//
// PullEvents clears as it reads, so calling this twice cannot republish. The
// service never constructs an event itself — it only forwards what the aggregate
// recorded, which is why an event exists exactly when a transition happened.
func (s *Service) publish(ctx context.Context, p *entity.Product) {
	for _, event := range p.PullEvents() {
		s.events.Publish(ctx, event)
	}
}

// buildMeasurements turns the request's optional strings into the aggregate's
// value objects. A nil field becomes a nil measurement (cleared); a present one
// is validated by its constructor. The dimension is all-or-nothing: supplying
// some sides but not all is a client error, caught here before the aggregate.
func buildMeasurements(
	req dto.SetMeasurementsRequest,
) (weight *entity.Weight, dimension *entity.Dimension, volume *entity.Volume, err error) {
	if req.Weight != nil {
		w, e := entity.NewWeightKilograms(*req.Weight)
		if e != nil {
			return nil, nil, nil, e
		}
		weight = &w
	}
	if req.Volume != nil {
		v, e := entity.NewVolumeCubicMetres(*req.Volume)
		if e != nil {
			return nil, nil, nil, e
		}
		volume = &v
	}
	if req.Dimension != nil {
		width, e := entity.NewLengthCentimetres(req.Dimension.Width)
		if e != nil {
			return nil, nil, nil, e
		}
		height, e := entity.NewLengthCentimetres(req.Dimension.Height)
		if e != nil {
			return nil, nil, nil, e
		}
		length, e := entity.NewLengthCentimetres(req.Dimension.Length)
		if e != nil {
			return nil, nil, nil, e
		}
		d, e := entity.NewDimension(width, height, length)
		if e != nil {
			return nil, nil, nil, e
		}
		dimension = &d
	}
	return weight, dimension, volume, nil
}
