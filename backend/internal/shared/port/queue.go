package port

import (
	"context"
	"encoding/json"
	"time"
)

// Priority is a broker-agnostic urgency level.
//
// Asynq expresses priority as weighted named queues; RabbitMQ uses a numeric
// x-max-priority header; SQS uses separate queues entirely. Business code must
// not know which. It says "this is urgent" and the adapter decides how that
// maps onto the broker it fronts.
type Priority string

const (
	PriorityCritical Priority = "critical"
	PriorityDefault  Priority = "default"
	PriorityLow      Priority = "low"
)

// Task is a unit of background work.
//
// Payload is bytes rather than `any` so the adapter never chooses a
// serialisation format on the caller's behalf; use NewTask to encode JSON.
type Task struct {
	// Type routes the task to its handler, e.g. "stock.recount".
	Type string
	// Payload is the encoded task arguments.
	Payload []byte
}

// NewTask builds a Task with a JSON-encoded payload.
func NewTask(taskType string, payload any) (Task, error) {
	if payload == nil {
		return Task{Type: taskType}, nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return Task{}, err
	}

	return Task{Type: taskType, Payload: raw}, nil
}

// EnqueueOptions controls delivery. Every field is expressed in neutral terms
// so any broker can honour or approximate it.
type EnqueueOptions struct {
	Priority Priority
	// Delay schedules the task relative to now. Ignored when ProcessAt is set.
	Delay time.Duration
	// ProcessAt schedules the task at an absolute time.
	ProcessAt time.Time
	// MaxRetry is the number of retries after the first failure.
	MaxRetry int
	// Timeout bounds a single execution attempt.
	Timeout time.Duration
	// Deadline discards the task if it has not succeeded by this time.
	Deadline time.Time
	// UniqueFor deduplicates identical tasks within the window. It is what
	// makes an at-least-once broker safe for non-idempotent work.
	UniqueFor time.Duration
	// ID assigns an explicit task identifier for idempotent enqueueing.
	ID string
}

// EnqueueOption mutates EnqueueOptions. The functional-option shape lets new
// delivery controls be added without breaking existing call sites.
type EnqueueOption func(*EnqueueOptions)

func WithPriority(p Priority) EnqueueOption {
	return func(o *EnqueueOptions) { o.Priority = p }
}

func WithDelay(d time.Duration) EnqueueOption {
	return func(o *EnqueueOptions) { o.Delay = d }
}

func WithProcessAt(t time.Time) EnqueueOption {
	return func(o *EnqueueOptions) { o.ProcessAt = t }
}

func WithMaxRetry(n int) EnqueueOption {
	return func(o *EnqueueOptions) { o.MaxRetry = n }
}

func WithTimeout(d time.Duration) EnqueueOption {
	return func(o *EnqueueOptions) { o.Timeout = d }
}

func WithDeadline(t time.Time) EnqueueOption {
	return func(o *EnqueueOptions) { o.Deadline = t }
}

func WithUniqueFor(d time.Duration) EnqueueOption {
	return func(o *EnqueueOptions) { o.UniqueFor = d }
}

func WithID(id string) EnqueueOption {
	return func(o *EnqueueOptions) { o.ID = id }
}

// ApplyEnqueueOptions folds opts over the defaults. Adapters call this so
// default behaviour stays identical no matter which broker is configured.
func ApplyEnqueueOptions(opts []EnqueueOption) EnqueueOptions {
	options := EnqueueOptions{
		Priority: PriorityDefault,
		MaxRetry: 3,
		Timeout:  5 * time.Minute,
	}

	for _, opt := range opts {
		opt(&options)
	}

	return options
}

// TaskInfo describes an accepted task.
type TaskInfo struct {
	ID         string
	Type       string
	Queue      string
	EnqueuedAt time.Time
	ProcessAt  time.Time
}

// Queue is the producer side of background processing.
//
// Only enqueueing is exposed. Consumption is the worker binary's concern, and
// keeping it off this interface means a module can never accidentally start
// processing jobs inside an HTTP request.
type Queue interface {
	// Enqueue submits a task for background execution.
	Enqueue(ctx context.Context, task Task, opts ...EnqueueOption) (TaskInfo, error)
}
