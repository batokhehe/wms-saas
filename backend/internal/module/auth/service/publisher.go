package service

import (
	"context"

	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/auth/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
)

// EventPublisher publishes domain events.
//
// It is declared here, in the consumer, per the project's consumer-side
// interface convention. Only publication exists in this sprint: no handlers are
// implemented, and none are needed for the events to be useful as an audit
// trail.
type EventPublisher interface {
	Publish(ctx context.Context, event entity.Event)
}

// LogEventPublisher writes domain events to the structured log.
//
// # Why the log rather than the Asynq queue
//
// The queue is the obvious choice and is the wrong one *today*. Asynq fails a
// task whose type has no registered handler, retries it, and finally archives
// it — so with no consumer implemented, every login would generate a failed
// task and an operator would open a dashboard full of red. Publishing into a
// broker with no subscriber manufactures alert noise, not an audit trail.
//
// The structured log is a real audit sink: lines are already shipped to a
// central store, already carry request_id, service and environment, and are
// already queryable. An audit event is a fact that happened, and recording it
// durably is the requirement — asynchronous delivery is not.
//
// The interface is the point. When Sprint 2 adds handlers, a QueueEventPublisher
// backed by port.Queue is one new type and one line in module.go; no service
// code changes. Publishing to both is then also a one-line composite.
//
// Publish returns no error, deliberately. An audit write must never fail the
// business operation that produced it: a login that succeeded has succeeded,
// and refusing to return the token because the audit sink hiccuped would turn a
// logging problem into an outage.
type LogEventPublisher struct {
	log *zap.Logger
}

var _ EventPublisher = (*LogEventPublisher)(nil)

// NewLogEventPublisher builds the publisher.
func NewLogEventPublisher(log *zap.Logger) *LogEventPublisher {
	// A dedicated component field so audit lines can be routed to their own
	// index and retained on a different schedule from application logs.
	return &LogEventPublisher{log: log.With(zap.String("component", "audit"))}
}

// Publish records a domain event.
func (p *LogEventPublisher) Publish(ctx context.Context, event entity.Event) {
	fields := []zap.Field{
		zap.String("event", string(event.Name)),
		zap.String("user_id", event.UserID.String()),
		zap.Time("occurred_at", event.OccurredAt),
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
