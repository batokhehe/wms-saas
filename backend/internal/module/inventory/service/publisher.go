package service

import (
	"context"

	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
)

// EventPublisher publishes inventory domain events. Declared here in the
// consumer, matching every other module.
type EventPublisher interface {
	Publish(ctx context.Context, event entity.Event)
}

// LogEventPublisher writes inventory events to the structured log.
//
// Same reasoning as the other modules' publishers: publishing into a broker with
// no subscriber would fill an operator's dashboard with dead-task errors, so
// until a real consumer exists the durable audit trail is the log. These events
// are raised by the AGGREGATE and pulled by the service AFTER the transaction
// commits, so an event is only ever published for a transition that actually
// persisted.
type LogEventPublisher struct {
	log *zap.Logger
}

var _ EventPublisher = (*LogEventPublisher)(nil)

// NewLogEventPublisher builds the publisher.
func NewLogEventPublisher(log *zap.Logger) *LogEventPublisher {
	return &LogEventPublisher{log: log.With(zap.String("component", "audit"))}
}

// Publish records a domain event.
func (p *LogEventPublisher) Publish(ctx context.Context, event entity.Event) {
	// Every event field is prefixed "event_", including the identifiers. Without
	// the prefix, event company/actor ids collide with the ones the
	// request-scoped logger already carries and zap emits BOTH — duplicate JSON
	// keys, which parsers handle inconsistently.
	fields := []zap.Field{
		zap.String("event", string(event.Name)),
		zap.String("event_inventory_id", event.InventoryID.String()),
		zap.String("event_company_id", event.CompanyID.String()),
		zap.String("event_actor_id", event.ActorID.String()),
		zap.Time("event_occurred_at", event.OccurredAt),
	}

	if requestID := appcontext.RequestID(ctx); requestID != "" {
		fields = append(fields, zap.String("event_request_id", requestID))
	}
	for key, value := range event.Attributes {
		fields = append(fields, zap.Any("event_"+key, value))
	}

	logger := p.log
	if requestLogger := appcontext.Logger(ctx); requestLogger != nil {
		logger = requestLogger.With(zap.String("component", "audit"))
	}

	logger.Info("domain event", fields...)
}
