// Package service holds the health module's business logic.
//
// It imports no Gin and no GORM, which is why it can be tested with fake
// checkers and no server, database or Redis. That property is the point of the
// layering, and it is enforced here by the import list.
package service

import (
	"context"
	"sync"
	"time"

	"github.com/batokhehe/wms-saas/backend/internal/module/health/dto"
)

// Checker is anything whose availability can be probed. Postgres and Redis both
// satisfy it, and a future dependency joins the readiness probe by implementing
// this single method.
type Checker interface {
	Health(ctx context.Context) error
}

// CheckerFunc adapts a plain function into a Checker.
type CheckerFunc func(ctx context.Context) error

func (f CheckerFunc) Health(ctx context.Context) error { return f(ctx) }

// dependency pairs a Checker with the name it reports under.
type dependency struct {
	name    string
	checker Checker
}

// Service aggregates dependency checks. It holds no mutable state after
// construction, so it is safe for concurrent use.
type Service struct {
	service   string
	version   string
	env       string
	startedAt time.Time

	deps []dependency

	// checkTimeout bounds each individual probe. A hung database must not hold
	// the readiness endpoint open until the caller gives up.
	checkTimeout time.Duration
}

// New builds the health service.
func New(serviceName, version, env string) *Service {
	return &Service{
		service:      serviceName,
		version:      version,
		env:          env,
		startedAt:    time.Now(),
		checkTimeout: 2 * time.Second,
	}
}

// Register adds a dependency to the readiness probe. It is called during
// bootstrap, before the server serves, so no locking is required.
func (s *Service) Register(name string, checker Checker) {
	s.deps = append(s.deps, dependency{name: name, checker: checker})
}

// SetCheckTimeout overrides the per-check budget. Used by tests.
func (s *Service) SetCheckTimeout(d time.Duration) { s.checkTimeout = d }

// Live reports process liveness. It deliberately checks nothing external: if a
// liveness probe failed because the database was down, the orchestrator would
// restart every replica during a database outage, turning a recoverable
// incident into a total one.
func (s *Service) Live() dto.HealthResponse {
	return dto.HealthResponse{
		Status:     dto.StatusUp,
		Service:    s.service,
		Version:    s.version,
		Env:        s.env,
		UptimeSecs: int64(time.Since(s.startedAt).Seconds()),
		CheckedAt:  time.Now().UTC(),
	}
}

// Ready probes every registered dependency concurrently and reports the
// aggregate. Concurrency keeps total latency at the slowest single check rather
// than the sum of all of them.
func (s *Service) Ready(ctx context.Context) dto.HealthResponse {
	report := s.Live()
	report.Components = make(map[string]dto.ComponentResponse, len(s.deps))

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	for _, dep := range s.deps {
		wg.Add(1)

		go func(dep dependency) {
			defer wg.Done()

			checkCtx, cancel := context.WithTimeout(ctx, s.checkTimeout)
			defer cancel()

			start := time.Now()
			err := dep.checker.Health(checkCtx)
			latency := time.Since(start)

			result := dto.ComponentResponse{
				Status:  dto.StatusUp,
				Latency: latency.Round(time.Millisecond).String(),
			}
			if err != nil {
				result.Status = dto.StatusDown
				result.Error = err.Error()
			}

			mu.Lock()
			report.Components[dep.name] = result
			mu.Unlock()
		}(dep)
	}

	wg.Wait()

	// One failing dependency makes the whole instance not ready, which removes
	// it from the load balancer without terminating it.
	for _, component := range report.Components {
		if component.Status == dto.StatusDown {
			report.Status = dto.StatusDown
			break
		}
	}

	return report
}
