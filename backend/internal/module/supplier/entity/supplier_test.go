package entity

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func act() (uuid.UUID, time.Time) { return uuid.New(), time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC) }

func mustCode(t *testing.T, raw string) SupplierCode {
	t.Helper()
	c, err := NewSupplierCode(raw)
	if err != nil {
		t.Fatalf("NewSupplierCode(%q) = %v", raw, err)
	}
	return c
}

// build constructs an ACTIVE supplier and discards the creation event.
func build(t *testing.T) *Supplier {
	t.Helper()
	actor, now := act()
	s, err := NewSupplier(uuid.New(), uuid.New(), mustCode(t, "SUP-1"), "Acme Traders",
		NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now)
	if err != nil {
		t.Fatalf("NewSupplier() = %v", err)
	}
	s.PullEvents()
	return s
}

func onlyEvent(t *testing.T, s *Supplier) Event {
	t.Helper()
	events := s.PullEvents()
	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %d", len(events))
	}
	return events[0]
}

func hasEvent(s *Supplier, name EventName) bool {
	for _, e := range s.PullEvents() {
		if e.Name == name {
			return true
		}
	}
	return false
}

// ---------- Factory ----------

func TestNewSupplierStartsActive(t *testing.T) {
	actor, now := act()
	email, _ := NewEmail("hello@acme.test")
	phone, _ := NewPhone("+62-811-1")
	tax, _ := NewTaxNumber("NPWP-123")
	addr, _ := NewAddress("Jl. Sudirman 1", "Jakarta", "DKI", "ID", "10110")

	s, err := NewSupplier(uuid.New(), uuid.New(), mustCode(t, "sup-01"), "Acme Traders",
		email, phone, tax, addr, actor, now)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status() != StatusActive {
		t.Errorf("status = %q, want ACTIVE", s.Status())
	}
	if s.Code().String() != "SUP-01" {
		t.Errorf("code = %q, want canonicalised SUP-01", s.Code())
	}
	if s.Version() != 1 || !s.IsActive() {
		t.Error("a new supplier should be version 1 and active")
	}
	if s.Email().String() != "hello@acme.test" || s.Address().City() != "Jakarta" {
		t.Error("contact/address did not survive construction")
	}
	ev := onlyEvent(t, s)
	if ev.Name != EventSupplierCreated || ev.Attributes["code"] != "SUP-01" {
		t.Fatalf("creation event wrong: %+v", ev.Attributes)
	}
}

func TestNewSupplierRejectsBadInput(t *testing.T) {
	actor, now := act()
	code := mustCode(t, "SUP-1")

	// nil ids
	if _, err := NewSupplier(uuid.Nil, uuid.New(), code, "Acme", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now); err == nil {
		t.Error("nil id accepted")
	}
	if _, err := NewSupplier(uuid.New(), uuid.New(), code, "Acme", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), uuid.Nil, now); err == nil {
		t.Error("nil actor accepted")
	}
	// blank name
	if _, err := NewSupplier(uuid.New(), uuid.New(), code, " ", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now); err == nil {
		t.Error("blank name accepted")
	}
	// empty code
	if _, err := NewSupplier(uuid.New(), uuid.New(), SupplierCode{}, "Acme", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now); err == nil {
		t.Error("empty code accepted")
	}
}

// ---------- Reconstitution ----------

func TestReconstituteRaisesNoEvents(t *testing.T) {
	actor, now := act()
	s, err := Reconstitute(uuid.New(), uuid.New(), mustCode(t, "SUP-1"), "Acme",
		NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), StatusInactive, 5, actor, actor, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.PullEvents(); len(got) != 0 {
		t.Errorf("reconstitution raised %d events, want 0", len(got))
	}
	if s.Version() != 5 || s.Status() != StatusInactive {
		t.Error("reconstituted state wrong")
	}
}

func TestReconstituteRejectsInvalidState(t *testing.T) {
	actor, now := act()
	code := mustCode(t, "SUP-1")
	cases := map[string]func() (*Supplier, error){
		"version zero": func() (*Supplier, error) {
			return Reconstitute(uuid.New(), uuid.New(), code, "Acme", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), StatusActive, 0, actor, actor, now, now)
		},
		"invalid status": func() (*Supplier, error) {
			return Reconstitute(uuid.New(), uuid.New(), code, "Acme", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), Status("X"), 1, actor, actor, now, now)
		},
		"blank name": func() (*Supplier, error) {
			return Reconstitute(uuid.New(), uuid.New(), code, "", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), StatusActive, 1, actor, actor, now, now)
		},
	}
	for label, fn := range cases {
		if _, err := fn(); err == nil {
			t.Errorf("%s: expected rejection", label)
		}
	}
}

// ---------- Behaviours ----------

func TestUpdateReplacesAttributesAndKeepsCode(t *testing.T) {
	s := build(t)
	actor, now := act()
	email, _ := NewEmail("new@acme.test")
	if err := s.Update("Acme International", email, NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now); err != nil {
		t.Fatal(err)
	}
	if s.Name() != "Acme International" || s.Email().String() != "new@acme.test" {
		t.Error("update did not apply")
	}
	if s.Code().String() != "SUP-1" {
		t.Error("update changed the code")
	}
	if !hasEvent(s, EventSupplierUpdated) {
		t.Error("no SupplierUpdated event")
	}
}

func TestUpdateRejectsBlankName(t *testing.T) {
	s := build(t)
	actor, now := act()
	if err := s.Update("", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now); err == nil {
		t.Fatal("blank name accepted on update")
	}
}

func TestActivateDeactivateCycle(t *testing.T) {
	s := build(t)
	actor, now := act()

	// Already active: idempotent no-op, no event.
	s.Activate(actor, now)
	if got := s.PullEvents(); len(got) != 0 {
		t.Error("activating an active supplier emitted an event")
	}

	s.Deactivate(actor, now)
	if s.IsActive() {
		t.Error("Deactivate did not deactivate")
	}
	if ev := onlyEvent(t, s); ev.Name != EventSupplierDeactivated {
		t.Errorf("event = %q, want deactivated", ev.Name)
	}
	// Deactivate again: idempotent.
	s.Deactivate(actor, now)
	if got := s.PullEvents(); len(got) != 0 {
		t.Error("deactivating an inactive supplier emitted an event")
	}

	s.Activate(actor, now)
	if !s.IsActive() {
		t.Error("Activate did not activate")
	}
	if ev := onlyEvent(t, s); ev.Name != EventSupplierActivated {
		t.Errorf("event = %q, want activated", ev.Name)
	}
}

func TestVersionIsNeverMutatedByBehaviours(t *testing.T) {
	s := build(t)
	actor, now := act()
	start := s.Version()
	_ = s.Update("Renamed", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now)
	s.Deactivate(actor, now)
	s.Activate(actor, now)
	if s.Version() != start {
		t.Fatalf("a behaviour changed Version: got %d, want %d — the repository owns it", s.Version(), start)
	}
}

// ---------- Value objects ----------

func TestSupplierCodeValidation(t *testing.T) {
	if _, err := NewSupplierCode("x"); err == nil {
		t.Error("single-char code accepted")
	}
	if c, _ := NewSupplierCode("  sup-9 "); c.String() != "SUP-9" {
		t.Errorf("code not canonicalised: %q", c.String())
	}
}

func TestEmailValidation(t *testing.T) {
	for _, bad := range []string{"", "no-at", "a@b", "a b@c.com"} {
		if _, err := NewEmail(bad); err == nil {
			t.Errorf("bad email accepted: %q", bad)
		}
	}
	if e, _ := NewEmail("A@B.COM"); e.String() != "a@b.com" {
		t.Errorf("email not lower-cased: %q", e.String())
	}
	if !NoEmail().IsZero() {
		t.Error("NoEmail should be zero")
	}
}

func TestAddressIsOptionalAndBounded(t *testing.T) {
	if _, err := NewAddress("", "", "", "", ""); err != nil {
		t.Errorf("empty address rejected: %v", err)
	}
	if !EmptyAddress().IsZero() {
		t.Error("EmptyAddress should be zero")
	}
	long := make([]byte, 600)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := NewAddress(string(long), "", "", "", ""); err == nil {
		t.Error("over-long street accepted")
	}
}
