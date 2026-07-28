package service

import (
	"context"

	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/location/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
)

// EventPublisher publishes location domain events.
//
// Declared here in the consumer, matching every other module.
type EventPublisher interface {
	Publish(ctx context.Context, event entity.Event)
}

// LogEventPublisher writes location events to the structured log.
//
// Same reasoning as the other modules' publishers: Asynq fails and archives
// tasks whose type has no registered handler, so publishing into a broker with
// no subscriber would fill an operator's dashboard with red.
//
// Like the warehouse module's, these events are raised by the AGGREGATE rather
// than the service, so an event exists because a transition happened rather
// than because a service method remembered to record one.
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
	// Every event field is prefixed "event_", including the identifiers.
	// Without the prefix, event company ids collide with the one the
	// request-scoped logger already carries and zap emits BOTH — a JSON object
	// with duplicate keys, which parsers handle inconsistently. The tenancy
	// sprint hit exactly that.
	fields := []zap.Field{
		zap.String("event", string(event.Name)),
		zap.String("event_location_id", event.LocationID.String()),
		zap.String("event_warehouse_id", event.WarehouseID.String()),
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
