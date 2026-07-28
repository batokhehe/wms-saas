package service

import (
	"context"

	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/customer/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
)

// EventPublisher publishes customer domain events. Declared here in the consumer,
// matching every other module.
type EventPublisher interface {
	Publish(ctx context.Context, event entity.Event)
}

// LogEventPublisher writes customer events to the structured log. Events are
// raised by the AGGREGATE and pulled by the service after commit, so an event
// exists because a transition persisted.
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
	fields := []zap.Field{
		zap.String("event", string(event.Name)),
		zap.String("event_customer_id", event.CustomerID.String()),
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
