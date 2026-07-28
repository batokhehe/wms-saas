package entity

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func act() (uuid.UUID, time.Time) { return uuid.New(), time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC) }

func mustCode(t *testing.T, raw string) CustomerCode {
	t.Helper()
	c, err := NewCustomerCode(raw)
	if err != nil {
		t.Fatalf("NewCustomerCode(%q) = %v", raw, err)
	}
	return c
}

// build constructs an ACTIVE customer and discards the creation event.
func build(t *testing.T) *Customer {
	t.Helper()
	actor, now := act()
	c, err := NewCustomer(uuid.New(), uuid.New(), mustCode(t, "CUS-1"), "Acme Retail",
		NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now)
	if err != nil {
		t.Fatalf("NewCustomer() = %v", err)
	}
	c.PullEvents()
	return c
}

func onlyEvent(t *testing.T, c *Customer) Event {
	t.Helper()
	events := c.PullEvents()
	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %d", len(events))
	}
	return events[0]
}

func hasEvent(c *Customer, name EventName) bool {
	for _, e := range c.PullEvents() {
		if e.Name == name {
			return true
		}
	}
	return false
}

// ---------- Factory ----------

func TestNewCustomerStartsActive(t *testing.T) {
	actor, now := act()
	email, _ := NewEmail("hello@acme.test")
	phone, _ := NewPhone("+62-811-1")
	tax, _ := NewTaxNumber("NPWP-123")
	addr, _ := NewAddress("Jl. Sudirman 1", "Jakarta", "DKI", "ID", "10110")

	c, err := NewCustomer(uuid.New(), uuid.New(), mustCode(t, "cus-01"), "Acme Retail",
		email, phone, tax, addr, actor, now)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status() != StatusActive {
		t.Errorf("status = %q, want ACTIVE", c.Status())
	}
	if c.Code().String() != "CUS-01" {
		t.Errorf("code = %q, want canonicalised CUS-01", c.Code())
	}
	if c.Version() != 1 || !c.IsActive() {
		t.Error("a new customer should be version 1 and active")
	}
	if c.Email().String() != "hello@acme.test" || c.Address().City() != "Jakarta" {
		t.Error("contact/address did not survive construction")
	}
	ev := onlyEvent(t, c)
	if ev.Name != EventCustomerCreated || ev.Attributes["code"] != "CUS-01" {
		t.Fatalf("creation event wrong: %+v", ev.Attributes)
	}
}

func TestNewCustomerRejectsBadInput(t *testing.T) {
	actor, now := act()
	code := mustCode(t, "CUS-1")

	if _, err := NewCustomer(uuid.Nil, uuid.New(), code, "Acme", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now); err == nil {
		t.Error("nil id accepted")
	}
	if _, err := NewCustomer(uuid.New(), uuid.New(), code, "Acme", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), uuid.Nil, now); err == nil {
		t.Error("nil actor accepted")
	}
	if _, err := NewCustomer(uuid.New(), uuid.New(), code, " ", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now); err == nil {
		t.Error("blank name accepted")
	}
	if _, err := NewCustomer(uuid.New(), uuid.New(), CustomerCode{}, "Acme", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now); err == nil {
		t.Error("empty code accepted")
	}
}

// ---------- Reconstitution ----------

func TestReconstituteRaisesNoEvents(t *testing.T) {
	actor, now := act()
	c, err := Reconstitute(uuid.New(), uuid.New(), mustCode(t, "CUS-1"), "Acme",
		NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), StatusInactive, 5, actor, actor, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.PullEvents(); len(got) != 0 {
		t.Errorf("reconstitution raised %d events, want 0", len(got))
	}
	if c.Version() != 5 || c.Status() != StatusInactive {
		t.Error("reconstituted state wrong")
	}
}

func TestReconstituteRejectsInvalidState(t *testing.T) {
	actor, now := act()
	code := mustCode(t, "CUS-1")
	cases := map[string]func() (*Customer, error){
		"version zero": func() (*Customer, error) {
			return Reconstitute(uuid.New(), uuid.New(), code, "Acme", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), StatusActive, 0, actor, actor, now, now)
		},
		"invalid status": func() (*Customer, error) {
			return Reconstitute(uuid.New(), uuid.New(), code, "Acme", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), Status("X"), 1, actor, actor, now, now)
		},
		"blank name": func() (*Customer, error) {
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
	c := build(t)
	actor, now := act()
	email, _ := NewEmail("new@acme.test")
	if err := c.Update("Acme Retail International", email, NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now); err != nil {
		t.Fatal(err)
	}
	if c.Name() != "Acme Retail International" || c.Email().String() != "new@acme.test" {
		t.Error("update did not apply")
	}
	if c.Code().String() != "CUS-1" {
		t.Error("update changed the code")
	}
	if !hasEvent(c, EventCustomerUpdated) {
		t.Error("no CustomerUpdated event")
	}
}

func TestUpdateRejectsBlankName(t *testing.T) {
	c := build(t)
	actor, now := act()
	if err := c.Update("", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now); err == nil {
		t.Fatal("blank name accepted on update")
	}
}

func TestActivateDeactivateCycle(t *testing.T) {
	c := build(t)
	actor, now := act()

	c.Activate(actor, now) // already active: no-op
	if got := c.PullEvents(); len(got) != 0 {
		t.Error("activating an active customer emitted an event")
	}

	c.Deactivate(actor, now)
	if c.IsActive() {
		t.Error("Deactivate did not deactivate")
	}
	if ev := onlyEvent(t, c); ev.Name != EventCustomerDeactivated {
		t.Errorf("event = %q, want deactivated", ev.Name)
	}
	c.Deactivate(actor, now) // idempotent
	if got := c.PullEvents(); len(got) != 0 {
		t.Error("deactivating an inactive customer emitted an event")
	}

	c.Activate(actor, now)
	if !c.IsActive() {
		t.Error("Activate did not activate")
	}
	if ev := onlyEvent(t, c); ev.Name != EventCustomerActivated {
		t.Errorf("event = %q, want activated", ev.Name)
	}
}

func TestVersionIsNeverMutatedByBehaviours(t *testing.T) {
	c := build(t)
	actor, now := act()
	start := c.Version()
	_ = c.Update("Renamed", NoEmail(), NoPhone(), NoTaxNumber(), EmptyAddress(), actor, now)
	c.Deactivate(actor, now)
	c.Activate(actor, now)
	if c.Version() != start {
		t.Fatalf("a behaviour changed Version: got %d, want %d", c.Version(), start)
	}
}

// ---------- Value objects ----------

func TestCustomerCodeValidation(t *testing.T) {
	if _, err := NewCustomerCode("x"); err == nil {
		t.Error("single-char code accepted")
	}
	if c, _ := NewCustomerCode("  cus-9 "); c.String() != "CUS-9" {
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
