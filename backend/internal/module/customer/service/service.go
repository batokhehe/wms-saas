package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/customer/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// mutation is a single domain call on a loaded aggregate.
type mutation func(*entity.Customer, uuid.UUID, time.Time) error

// Service orchestrates the Customer aggregate — the structural sibling of the
// supplier service. It holds no business rules; the code-uniqueness set rule is a
// specification, everything else is the aggregate's.
type Service struct {
	repo     repository.Repository
	codeSpec UniqueCustomerCode
	clock    port.Clock
	ids      port.IDGenerator
	tx       transaction.Manager
	events   EventPublisher
}

// New builds the service.
func New(
	repo repository.Repository,
	clock port.Clock,
	ids port.IDGenerator,
	tx transaction.Manager,
	events EventPublisher,
) *Service {
	return &Service{
		repo:     repo,
		codeSpec: NewUniqueCustomerCode(repo),
		clock:    clock,
		ids:      ids,
		tx:       tx,
		events:   events,
	}
}

// actor resolves the tenant and the acting user together, both from the
// RequestContext.
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

// Create registers a customer.
func (s *Service) Create(ctx context.Context, req dto.CreateCustomerRequest) (dto.CustomerResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.CustomerResponse{}, err
	}

	code, err := entity.NewCustomerCode(req.Code)
	if err != nil {
		return dto.CustomerResponse{}, err
	}
	email, phone, tax, err := buildContact(req.Email, req.Phone, req.TaxNumber)
	if err != nil {
		return dto.CustomerResponse{}, err
	}
	address, err := entity.NewAddress(req.Address, req.City, req.Province, req.Country, req.PostalCode)
	if err != nil {
		return dto.CustomerResponse{}, err
	}

	var customer *entity.Customer

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.codeSpec.Satisfy(ctx, companyID, code.String()); err != nil {
			return err
		}
		created, err := entity.NewCustomer(
			s.ids.NewID(), companyID, code, req.Name, email, phone, tax, address, actorID, s.clock.Now(),
		)
		if err != nil {
			return err
		}
		if err := s.repo.Save(ctx, created); err != nil {
			return err
		}
		customer = created
		return nil
	})
	if err != nil {
		return dto.CustomerResponse{}, err
	}

	s.publish(ctx, customer)
	return mapper.ToResponse(customer), nil
}

// Get returns one customer.
func (s *Service) Get(ctx context.Context, customerID uuid.UUID) (dto.CustomerResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.CustomerResponse{}, err
	}
	customer, err := s.repo.FindByID(ctx, customerID, companyID)
	if err != nil {
		return dto.CustomerResponse{}, err
	}
	return mapper.ToResponse(customer), nil
}

// List returns a page of the company's customers.
func (s *Service) List(ctx context.Context, query dto.ListCustomersQuery) (pagination.Page[dto.CustomerResponse], error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return pagination.Page[dto.CustomerResponse]{}, err
	}
	if err := query.Request.Apply(dto.SortOptions()); err != nil {
		return pagination.Page[dto.CustomerResponse]{}, err
	}
	page, err := s.repo.List(ctx, companyID, repository.ListFilter{
		Paging: query.Request,
		Status: query.Status,
	})
	if err != nil {
		return pagination.Page[dto.CustomerResponse]{}, err
	}
	return mapper.ToPage(page), nil
}

// Update replaces a customer's mutable attributes.
func (s *Service) Update(ctx context.Context, customerID uuid.UUID, req dto.UpdateCustomerRequest) (dto.CustomerResponse, error) {
	email, phone, tax, err := buildContact(req.Email, req.Phone, req.TaxNumber)
	if err != nil {
		return dto.CustomerResponse{}, err
	}
	address, err := entity.NewAddress(req.Address, req.City, req.Province, req.Country, req.PostalCode)
	if err != nil {
		return dto.CustomerResponse{}, err
	}
	return s.mutate(ctx, customerID, "Update", func(cust *entity.Customer, actor uuid.UUID, now time.Time) error {
		return cust.Update(req.Name, email, phone, tax, address, actor, now)
	})
}

// Activate makes a customer selectable for new orders.
func (s *Service) Activate(ctx context.Context, customerID uuid.UUID) (dto.CustomerResponse, error) {
	return s.mutate(ctx, customerID, "Activate", func(cust *entity.Customer, actor uuid.UUID, now time.Time) error {
		cust.Activate(actor, now)
		return nil
	})
}

// Deactivate removes a customer from selection for new orders.
func (s *Service) Deactivate(ctx context.Context, customerID uuid.UUID) (dto.CustomerResponse, error) {
	return s.mutate(ctx, customerID, "Deactivate", func(cust *entity.Customer, actor uuid.UUID, now time.Time) error {
		cust.Deactivate(actor, now)
		return nil
	})
}

// mutate is the shared shape of every load-modify-persist operation. Load, call
// one domain method, persist — inside one transaction — then publish after
// commit.
func (s *Service) mutate(ctx context.Context, customerID uuid.UUID, op string, apply mutation) (dto.CustomerResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.CustomerResponse{}, err
	}

	var customer *entity.Customer

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		loaded, err := s.repo.FindByID(ctx, customerID, companyID)
		if err != nil {
			return err
		}
		if !loaded.BelongsTo(companyID) {
			return apperror.Forbidden("This customer belongs to another company").
				WithOp("customer.service." + op)
		}
		if err := apply(loaded, actorID, s.clock.Now()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, loaded); err != nil {
			return err
		}
		customer = loaded
		return nil
	})
	if err != nil {
		return dto.CustomerResponse{}, s.concurrentModification(ctx, customerID, companyID, err)
	}

	s.publish(ctx, customer)
	return mapper.ToResponse(customer), nil
}

// concurrentModification turns the repository sentinel into the API's normal 409
// after rollback, carrying the current version as the retry token.
func (s *Service) concurrentModification(ctx context.Context, id, companyID uuid.UUID, err error) error {
	if err == nil || !errors.Is(err, sharedrepo.ErrConcurrentModification) {
		return err
	}
	current, readErr := s.repo.FindByID(ctx, id, companyID)
	if readErr != nil {
		return err
	}
	return apperror.Conflict("The resource was changed by another request").
		WithOp("customer.service.concurrentModification").
		WithDetails(map[string]any{"entity_id": id, "current_version": current.Version()}).
		WithCause(sharedrepo.ErrConcurrentModification)
}

// publish drains the aggregate's recorded events and publishes them.
func (s *Service) publish(ctx context.Context, cust *entity.Customer) {
	for _, event := range cust.PullEvents() {
		s.events.Publish(ctx, event)
	}
}

// buildContact turns the request's optional contact strings into value objects,
// treating an empty string as absence and validating a non-empty one.
func buildContact(email, phone, taxNumber string) (entity.Email, entity.Phone, entity.TaxNumber, error) {
	e := entity.NoEmail()
	if email != "" {
		built, err := entity.NewEmail(email)
		if err != nil {
			return entity.Email{}, entity.Phone{}, entity.TaxNumber{}, err
		}
		e = built
	}
	p := entity.NoPhone()
	if phone != "" {
		built, err := entity.NewPhone(phone)
		if err != nil {
			return entity.Email{}, entity.Phone{}, entity.TaxNumber{}, err
		}
		p = built
	}
	tax := entity.NoTaxNumber()
	if taxNumber != "" {
		built, err := entity.NewTaxNumber(taxNumber)
		if err != nil {
			return entity.Email{}, entity.Phone{}, entity.TaxNumber{}, err
		}
		tax = built
	}
	return e, p, tax, nil
}
