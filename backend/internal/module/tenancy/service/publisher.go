package service

import (
	"context"

	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
)

// EventPublisher publishes tenancy domain events.
//
// Declared here in the consumer, matching the auth module's shape. Only
// publication exists: no handlers are implemented, and none are needed for the
// events to be useful as an audit trail.
type EventPublisher interface {
	Publish(ctx context.Context, event entity.Event)
}

// LogEventPublisher writes tenancy events to the structured log.
//
// The reasoning is identical to the auth module's publisher and is repeated
// here only in summary: Asynq fails and archives tasks whose type has no
// registered handler, so publishing into a broker with no subscriber would fill
// an operator's dashboard with red. The structured log is a real audit sink —
// already shipped centrally, already carrying request_id, already queryable.
//
// Swapping to a queue-backed publisher when Sprint 3 adds handlers is one new
// type and one line in module.go; no service code changes.
//
// Publish returns no error deliberately: an audit write must never fail the
// business operation that produced it.
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
	// Every event-specific field is prefixed "event_", including the identifiers.
	//
	// Without the prefix, event_company_id collides with the company_id the
	// request-scoped logger already carries, and zap emits BOTH — producing a
	// JSON object with a duplicate key. Parsers handle that inconsistently: some
	// take the first value, some the last, some reject the line outright. It was
	// visible in the running service before this prefix was added.
	//
	// The prefix also keeps the distinction that matters for auditing: company_id
	// is the tenant the REQUEST ran in, while event_company_id is the tenant the
	// event is ABOUT. They are usually the same and must not be assumed to be.
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

	// Logged through the request-scoped logger so the event inherits the
	// request's correlation fields; falls back to the base logger outside a
	// request (a background job, a test).
	logger := p.log
	if requestLogger := appcontext.Logger(ctx); requestLogger != nil {
		logger = requestLogger.With(zap.String("component", "audit"))
	}

	logger.Info("domain event", fields...)
}
