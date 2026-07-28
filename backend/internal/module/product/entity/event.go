package entity

import (
	"github.com/google/uuid"
	"time"
)

type EventName string

const (
	EventProductCreated        EventName = "product.created"
	EventProductActivated      EventName = "product.activated"
	EventProductDeactivated    EventName = "product.deactivated"
	EventProductDiscontinued   EventName = "product.discontinued"
	EventProductRenamed        EventName = "product.renamed"
	EventBarcodeAdded          EventName = "product.barcode_added"
	EventBarcodeRemoved        EventName = "product.barcode_removed"
	EventPrimaryBarcodeChanged EventName = "product.primary_barcode_changed"
	EventTrackingMethodChanged EventName = "product.tracking_method_changed"
	EventCategoryAssigned      EventName = "product.category_assigned"
	EventBrandAssigned         EventName = "product.brand_assigned"
	// Child-collection and measurement mutations emit events for the same
	// reason the barcode operations do: an auditor, and any future
	// event-driven read model, must be able to reconstruct the alternate-UOM
	// set and the physical profile from the stream alone.
	EventUOMAdded            EventName = "product.uom_added"
	EventUOMRemoved          EventName = "product.uom_removed"
	EventMeasurementsChanged EventName = "product.measurements_changed"
	EventShelfLifeChanged    EventName = "product.shelf_life_changed"
)

// idString renders an optional identifier reference for an event payload.
//
// Event attributes must be immutable, self-describing values, never pointers
// into (or copies of pointers from) the aggregate: a consumer serialising a
// *uuid.UUID gets a nil or a reference it cannot reason about. "" denotes "no
// value", which category and brand assignment both need to express.
func idString(v *uuid.UUID) string {
	if v == nil {
		return ""
	}
	return v.String()
}

type Event struct {
	Name                          EventName
	ProductID, CompanyID, ActorID uuid.UUID
	OccurredAt                    time.Time
	Attributes                    map[string]any
}

func (p *Product) record(name EventName, actor uuid.UUID, now time.Time, attributes map[string]any) {
	p.events = append(p.events, Event{Name: name, ProductID: p.id, CompanyID: p.companyID, ActorID: actor, OccurredAt: now, Attributes: attributes})
}
func (p *Product) PullEvents() []Event { out := p.events; p.events = nil; return out }
