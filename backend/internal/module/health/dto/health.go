// Package dto holds the health module's transport contracts.
//
// DTOs are the API's shape and change only when the API changes. Keeping them
// separate from domain types means an internal refactor cannot accidentally
// alter the JSON a released client depends on.
package dto

import "time"

// Status is the state of the service or one of its dependencies.
type Status string

const (
	StatusUp   Status = "up"
	StatusDown Status = "down"
)

// ComponentResponse is the per-dependency result.
type ComponentResponse struct {
	Status  Status `json:"status"`
	Latency string `json:"latency"`
	Error   string `json:"error,omitempty"`
}

// HealthResponse is the payload placed in the envelope's `data` field.
//
// It is a plain data structure with no envelope fields of its own: wrapping is
// the response package's job, and duplicating it here would break the
// one-response-format rule.
type HealthResponse struct {
	Status     Status                       `json:"status"`
	Service    string                       `json:"service"`
	Version    string                       `json:"version"`
	Env        string                       `json:"environment"`
	UptimeSecs int64                        `json:"uptime_seconds"`
	CheckedAt  time.Time                    `json:"checked_at"`
	Components map[string]ComponentResponse `json:"components,omitempty"`
}
