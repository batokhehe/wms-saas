package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
)

// TestLogEventPublisherStampsEventFacts verifies the publisher records the event
// name and identity under prefixed keys and flattens the aggregate's attributes.
//
// It drives Publish through a context carrying the observer logger, because the
// publisher deliberately prefers the request-scoped logger so every event line
// is attributable to the request that raised it.
func TestLogEventPublisherStampsEventFacts(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	rc := appcontext.New("req-1", zap.New(core))
	ctx := appcontext.Into(context.Background(), rc)

	publisher := NewLogEventPublisher(zap.NewNop())

	inventoryID, companyID, actorID := uuid.New(), uuid.New(), uuid.New()
	publisher.Publish(ctx, entity.Event{
		Name:       entity.EventStockReserved,
		PositionID: inventoryID,
		CompanyID:  companyID,
		ActorID:    actorID,
		OccurredAt: time.Now().UTC(),
		Attributes: map[string]any{"amount": int64(7), "available": int64(3)},
	})

	logs := recorded.All()
	if len(logs) != 1 {
		t.Fatalf("logged %d entries, want 1", len(logs))
	}
	fields := logs[0].ContextMap()

	if got := fields["event"]; got != string(entity.EventStockReserved) {
		t.Errorf("event = %v, want %s", got, entity.EventStockReserved)
	}
	if got := fields["event_position_id"]; got != inventoryID.String() {
		t.Errorf("event_position_id = %v, want %s", got, inventoryID)
	}
	if got := fields["event_company_id"]; got != companyID.String() {
		t.Errorf("event_company_id = %v", got)
	}
	// Aggregate attributes are flattened under the event_ prefix.
	if got := fields["event_amount"]; got != int64(7) {
		t.Errorf("event_amount = %v, want 7", got)
	}
	if got := fields["event_available"]; got != int64(3) {
		t.Errorf("event_available = %v, want 3", got)
	}
}
