// Package queue adapts Asynq to the port.Queue interface.
//
// This package is the only place that may import hibiken/asynq for producing
// tasks. Swapping in RabbitMQ means adding rabbitmq_queue.go here and changing
// one line in bootstrap; no business module is aware which broker is running.
package queue

import (
	"context"
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
)

// Asynq queue names. These are an Asynq implementation detail and are
// deliberately unexported: business code expresses urgency as port.Priority and
// never names a queue.
const (
	queueCritical = "critical"
	queueDefault  = "default"
	queueLow      = "low"
)

// Weights configures how often the worker drains each queue.
//
// Exported because the worker binary needs the same mapping the producer uses.
// A 6:3:1 split means a saturated low-priority backlog still leaves critical
// tasks moving.
var Weights = map[string]int{
	queueCritical: 6,
	queueDefault:  3,
	queueLow:      1,
}

// AsynqQueue implements port.Queue on top of Asynq.
type AsynqQueue struct {
	client *asynq.Client
}

var _ port.Queue = (*AsynqQueue)(nil)

// NewAsynqQueue builds the adapter.
func NewAsynqQueue(client *asynq.Client) *AsynqQueue {
	return &AsynqQueue{client: client}
}

// Enqueue translates a neutral Task into an Asynq task and submits it.
func (q *AsynqQueue) Enqueue(
	ctx context.Context,
	task port.Task,
	opts ...port.EnqueueOption,
) (port.TaskInfo, error) {
	options := port.ApplyEnqueueOptions(opts)

	asynqTask := asynq.NewTask(task.Type, task.Payload)

	info, err := q.client.EnqueueContext(ctx, asynqTask, buildOptions(options)...)
	if err != nil {
		return port.TaskInfo{}, fmt.Errorf("queue: enqueue %q: %w", task.Type, err)
	}

	return port.TaskInfo{
		ID:         info.ID,
		Type:       info.Type,
		Queue:      info.Queue,
		EnqueuedAt: info.NextProcessAt,
		ProcessAt:  info.NextProcessAt,
	}, nil
}

// buildOptions maps the neutral options onto Asynq's.
//
// Every mapping decision that would otherwise be scattered across call sites
// lives here, which is exactly what makes the broker swappable.
func buildOptions(o port.EnqueueOptions) []asynq.Option {
	opts := []asynq.Option{
		asynq.Queue(queueName(o.Priority)),
		asynq.MaxRetry(o.MaxRetry),
	}

	if o.Timeout > 0 {
		opts = append(opts, asynq.Timeout(o.Timeout))
	}
	if !o.Deadline.IsZero() {
		opts = append(opts, asynq.Deadline(o.Deadline))
	}
	if o.UniqueFor > 0 {
		opts = append(opts, asynq.Unique(o.UniqueFor))
	}
	if o.ID != "" {
		opts = append(opts, asynq.TaskID(o.ID))
	}

	// ProcessAt is absolute and wins over the relative Delay, matching the
	// documented precedence on port.EnqueueOptions.
	switch {
	case !o.ProcessAt.IsZero():
		opts = append(opts, asynq.ProcessAt(o.ProcessAt))
	case o.Delay > 0:
		opts = append(opts, asynq.ProcessIn(o.Delay))
	}

	return opts
}

// queueName maps a neutral priority onto an Asynq queue.
func queueName(p port.Priority) string {
	switch p {
	case port.PriorityCritical:
		return queueCritical
	case port.PriorityLow:
		return queueLow
	default:
		return queueDefault
	}
}
