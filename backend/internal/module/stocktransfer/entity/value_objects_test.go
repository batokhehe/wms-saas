package entity

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTransferNumberValidation(t *testing.T) {
	for _, bad := range []string{"", "   ", "ST", strings.Repeat("X", 33)} {
		if _, err := NewTransferNumber(bad); err == nil {
			t.Errorf("bad transfer number accepted: %q", bad)
		}
	}
	number, err := NewTransferNumber("  st-2026-001 ")
	if err != nil {
		t.Fatal(err)
	}
	if number.String() != "ST-2026-001" {
		t.Errorf("number not canonicalised: %q", number.String())
	}
	if !(TransferNumber{}).IsZero() {
		t.Error("the zero TransferNumber should report IsZero")
	}
}

// TestQuantityMustBePositive pins that zero is refused, not merely negatives: a
// zero-unit line moves nothing but would still be executed and recorded.
func TestQuantityMustBePositive(t *testing.T) {
	for _, bad := range []int64{0, -1, -100} {
		if _, err := NewQuantity(bad); err == nil {
			t.Errorf("non-positive quantity accepted: %d", bad)
		}
	}
	quantity, err := NewQuantity(12)
	if err != nil || quantity.Value() != 12 {
		t.Errorf("NewQuantity(12) = %v, %v", quantity.Value(), err)
	}
}

func TestStatusPredicates(t *testing.T) {
	if !StatusDraft.IsEditable() {
		t.Error("DRAFT must be editable")
	}
	for _, locked := range []Status{StatusConfirmed, StatusCompleted, StatusCancelled} {
		if locked.IsEditable() {
			t.Errorf("%s must not be editable", locked)
		}
	}
	for _, terminal := range []Status{StatusCompleted, StatusCancelled} {
		if !terminal.IsTerminal() {
			t.Errorf("%s must be terminal", terminal)
		}
	}
	for _, open := range []Status{StatusDraft, StatusConfirmed} {
		if open.IsTerminal() {
			t.Errorf("%s must not be terminal", open)
		}
	}
}

func TestNewStatusValidation(t *testing.T) {
	status, err := NewStatus(" confirmed ")
	if err != nil || status != StatusConfirmed {
		t.Errorf("NewStatus(\" confirmed \") = %q, %v", status, err)
	}
	if _, err := NewStatus("SHIPPED"); err == nil {
		t.Error("an unknown status was accepted")
	}
}

// TestSerialCountMustMatchQuantity is the tracking rule: a serial identifies one
// physical item and cannot stand for two.
func TestSerialCountMustMatchQuantity(t *testing.T) {
	quantity, err := NewQuantity(3)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewLineAttributes("", "", []string{"SN-1", "SN-2"}, quantity); err == nil {
		t.Error("two serials were accepted for a three-unit line")
	}
	if _, err := NewLineAttributes("", "", []string{"SN-1", "SN-2", "SN-3", "SN-4"}, quantity); err == nil {
		t.Error("four serials were accepted for a three-unit line")
	}
	attributes, err := NewLineAttributes("B-1", "LOT-9", []string{"SN-1", "SN-2", "SN-3"}, quantity)
	if err != nil {
		t.Fatal(err)
	}
	if !attributes.IsSerialTracked() || attributes.BatchNumber() != "B-1" || attributes.LotNumber() != "LOT-9" {
		t.Errorf("attributes not stored: %+v", attributes)
	}
}

func TestLineAttributesRejectDuplicateSerials(t *testing.T) {
	quantity, err := NewQuantity(2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewLineAttributes("", "", []string{"SN-1", "SN-1"}, quantity); err == nil {
		t.Error("the same serial was accepted twice on one line")
	}
}

// TestLineAttributesAreOptional pins that untracked stock needs no attributes.
func TestLineAttributesAreOptional(t *testing.T) {
	quantity, err := NewQuantity(10)
	if err != nil {
		t.Fatal(err)
	}
	attributes, err := NewLineAttributes("", "", nil, quantity)
	if err != nil {
		t.Fatalf("empty attributes rejected: %v", err)
	}
	if !attributes.IsZero() || attributes.IsSerialTracked() {
		t.Error("empty attributes should be zero and untracked")
	}
	if !NoLineAttributes().IsZero() {
		t.Error("NoLineAttributes should be zero")
	}

	// Blank serial entries are dropped rather than counted, so a trailing empty
	// field from a form does not break the count rule.
	padded, err := NewLineAttributes("", "", []string{"", "  "}, quantity)
	if err != nil || padded.IsSerialTracked() {
		t.Errorf("blank serials should be dropped: %v", err)
	}
}

func TestSerialNumbersAreDefensivelyCopied(t *testing.T) {
	quantity, err := NewQuantity(2)
	if err != nil {
		t.Fatal(err)
	}
	supplied := []string{"SN-1", "SN-2"}
	attributes, err := NewLineAttributes("", "", supplied, quantity)
	if err != nil {
		t.Fatal(err)
	}

	supplied[0] = "TAMPERED"
	if attributes.SerialNumbers()[0] != "SN-1" {
		t.Fatal("mutating the supplied slice changed the value object")
	}

	returned := attributes.SerialNumbers()
	returned[0] = "TAMPERED"
	if attributes.SerialNumbers()[0] != "SN-1" {
		t.Fatal("mutating the returned slice changed the value object")
	}
}

// ---------- Line ----------

func TestLineRejectsAMovementThatGoesNowhere(t *testing.T) {
	quantity, err := NewQuantity(1)
	if err != nil {
		t.Fatal(err)
	}
	location := uuid.New()
	if _, err := NewStockTransferLine(uuid.New(), uuid.New(), location, location, quantity, NoLineAttributes()); err == nil {
		t.Error("a line moving stock to where it already is was accepted")
	}
}

func TestLineRejectsMissingIdentifiers(t *testing.T) {
	quantity, err := NewQuantity(1)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func() error{
		"nil line id": func() error {
			_, err := NewStockTransferLine(uuid.Nil, uuid.New(), uuid.New(), uuid.New(), quantity, NoLineAttributes())
			return err
		},
		"nil product": func() error {
			_, err := NewStockTransferLine(uuid.New(), uuid.Nil, uuid.New(), uuid.New(), quantity, NoLineAttributes())
			return err
		},
		"nil source location": func() error {
			_, err := NewStockTransferLine(uuid.New(), uuid.New(), uuid.Nil, uuid.New(), quantity, NoLineAttributes())
			return err
		},
		"nil destination location": func() error {
			_, err := NewStockTransferLine(uuid.New(), uuid.New(), uuid.New(), uuid.Nil, quantity, NoLineAttributes())
			return err
		},
	}
	for label, fn := range cases {
		if err := fn(); err == nil {
			t.Errorf("%s: expected rejection", label)
		}
	}
}
