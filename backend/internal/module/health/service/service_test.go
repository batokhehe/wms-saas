package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/batokhehe/wms-saas/backend/internal/module/health/dto"
)

// These tests use fake checkers and no Gin, no database and no Redis. That is
// the practical payoff of keeping the service layer free of transport and
// persistence imports.

func TestLiveIgnoresDependencies(t *testing.T) {
	s := New("wms-saas", "1.0.0", "test")
	s.Register("postgres", CheckerFunc(func(context.Context) error {
		return errors.New("database is on fire")
	}))

	// Liveness must stay up even with a dead dependency; otherwise a database
	// outage would trigger a restart storm across every replica.
	if got := s.Live(); got.Status != dto.StatusUp {
		t.Errorf("Live().Status = %q, want %q", got.Status, dto.StatusUp)
	}
}

func TestReadyAllHealthy(t *testing.T) {
	s := New("wms-saas", "1.0.0", "test")
	s.Register("postgres", CheckerFunc(func(context.Context) error { return nil }))
	s.Register("redis", CheckerFunc(func(context.Context) error { return nil }))

	report := s.Ready(context.Background())

	if report.Status != dto.StatusUp {
		t.Errorf("Ready().Status = %q, want %q", report.Status, dto.StatusUp)
	}
	if got, want := len(report.Components), 2; got != want {
		t.Fatalf("len(Components) = %d, want %d", got, want)
	}
}

func TestReadyOneDependencyDown(t *testing.T) {
	s := New("wms-saas", "1.0.0", "test")
	s.Register("postgres", CheckerFunc(func(context.Context) error { return nil }))
	s.Register("redis", CheckerFunc(func(context.Context) error {
		return errors.New("connection refused")
	}))

	report := s.Ready(context.Background())

	// A single failing dependency must fail the whole probe, so the instance is
	// pulled from the load balancer rather than serving broken requests.
	if report.Status != dto.StatusDown {
		t.Errorf("Ready().Status = %q, want %q", report.Status, dto.StatusDown)
	}
	if report.Components["redis"].Error == "" {
		t.Error("Components[redis].Error is empty, want the failure reason")
	}
}

// TestReadyBoundsSlowChecks proves a hung dependency cannot hold the probe open
// indefinitely, and that checks run concurrently rather than serially.
func TestReadyBoundsSlowChecks(t *testing.T) {
	s := New("wms-saas", "1.0.0", "test")
	s.SetCheckTimeout(100 * time.Millisecond)

	hang := CheckerFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	s.Register("slow-a", hang)
	s.Register("slow-b", hang)

	start := time.Now()
	report := s.Ready(context.Background())
	elapsed := time.Since(start)

	if report.Status != dto.StatusDown {
		t.Errorf("Ready().Status = %q, want %q", report.Status, dto.StatusDown)
	}
	// Serial execution would take ~200ms; concurrent takes ~100ms.
	if elapsed > 180*time.Millisecond {
		t.Errorf("Ready() took %v, want checks to run concurrently under ~180ms", elapsed)
	}
}
