package service

import (
	"context"

	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
)

// EventPublisher publishes RBAC domain events.
//
// Declared here in the consumer, matching the auth and tenancy modules.
type EventPublisher interface {
	Publish(ctx context.Context, event entity.Event)
}

// LogEventPublisher writes RBAC events to the structured log.
//
// Same reasoning as the other modules' publishers: Asynq fails and archives
// tasks whose type has no registered handler, so publishing into a broker with
// no subscriber would fill an operator's dashboard with red. The structured log
// is a real audit sink — already shipped centrally, already carrying
// request_id, already queryable.
//
// These events matter more than most. A change to who may do what is the single
// most security-relevant mutation the system supports, and it is the first
// thing an incident review asks about.
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
	// Without the prefix, event company/user ids collide with the ones the
	// request-scoped logger already carries and zap emits BOTH — a JSON object
	// with duplicate keys, which parsers handle inconsistently. The tenancy
	// sprint hit exactly that.
	fields := []zap.Field{
		zap.String("event", string(event.Name)),
		zap.String("event_company_id", event.CompanyID.String()),
		zap.String("event_actor_id", event.ActorID.String()),
		zap.Time("event_occurred_at", event.OccurredAt),
	}

	if event.RequestID != "" {
		fields = append(fields, zap.String("event_request_id", event.RequestID))
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
