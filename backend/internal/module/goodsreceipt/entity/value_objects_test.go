package entity

import (
	"testing"

	"github.com/google/uuid"
)

func TestReceiptNumberValidation(t *testing.T) {
	for _, bad := range []string{"", "  ", "GR"} {
		if _, err := NewReceiptNumber(bad); err == nil {
			t.Errorf("bad receipt number accepted: %q", bad)
		}
	}
	number, err := NewReceiptNumber("  gr-2026-001 ")
	if err != nil {
		t.Fatal(err)
	}
	if number.String() != "GR-2026-001" {
		t.Errorf("number not canonicalised: %q", number.String())
	}
	if !(ReceiptNumber{}).IsZero() {
		t.Error("the zero ReceiptNumber should report IsZero")
	}
}

// TestQuantityMustBePositive pins that whole, positive units are the only valid
// receipt quantity — a zero arrival cannot reconcile with a stock position.
func TestQuantityMustBePositive(t *testing.T) {
	for _, bad := range []int64{0, -1} {
		if _, err := NewQuantity(bad); err == nil {
			t.Errorf("non-positive quantity accepted: %d", bad)
		}
	}
	if q, err := NewQuantity(9); err != nil || q.Value() != 9 {
		t.Errorf("NewQuantity(9) = %v, %v", q.Value(), err)
	}
}

func TestStatusPredicates(t *testing.T) {
	if !StatusDraft.IsEditable() {
		t.Error("DRAFT must be editable")
	}
	for _, locked := range []Status{StatusConfirmed, StatusReceived, StatusCancelled} {
		if locked.IsEditable() {
			t.Errorf("%s must not be editable", locked)
		}
	}
	for _, terminal := range []Status{StatusReceived, StatusCancelled} {
		if !terminal.IsTerminal() {
			t.Errorf("%s must be terminal", terminal)
		}
	}
	if _, err := NewStatus("SHIPPED"); err == nil {
		t.Error("an unknown status was accepted")
	}
	if s, err := NewStatus(" received "); err != nil || s != StatusReceived {
		t.Errorf("NewStatus(received) = %q, %v", s, err)
	}
}

// ---------- DocumentReference ----------

// TestReferenceRejectsIncoherentPairs pins that a type and an id are only
// meaningful together.
func TestReferenceRejectsIncoherentPairs(t *testing.T) {
	id := uuid.New()

	if _, err := NewDocumentReference(ReferencePurchaseOrder, nil); err == nil {
		t.Error("a PURCHASE_ORDER reference with no id was accepted")
	}
	if _, err := NewDocumentReference(ReferenceNone, &id); err == nil {
		t.Error("a NONE reference naming an id was accepted")
	}
	if _, err := NewDocumentReference(ReferenceType("INVOICE"), &id); err == nil {
		t.Error("an unknown reference type was accepted")
	}
}

func TestReferenceDefaultsToNone(t *testing.T) {
	reference, err := NewDocumentReference("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if reference.Kind() != ReferenceNone || reference.HasReference() {
		t.Error("an empty reference type should read as NONE with no reference")
	}
	if !NoReference().Kind().Valid() || NoReference().HasReference() {
		t.Error("NoReference should be a valid, empty reference")
	}
}

func TestPurchaseOrderReferenceIsRecognised(t *testing.T) {
	id := uuid.New()
	reference, err := NewDocumentReference(ReferencePurchaseOrder, &id)
	if err != nil {
		t.Fatal(err)
	}
	if !reference.IsPurchaseOrder() || !reference.HasReference() {
		t.Error("a purchase-order reference was not recognised")
	}
	// The returned pointer is a copy.
	returned := reference.ID()
	*returned = uuid.New()
	if *reference.ID() == *returned {
		t.Error("mutating the returned id changed the value object")
	}
}

// ---------- Line ----------

// TestLotAndSerialsAreMutuallyExclusive pins that a line cannot claim to be both
// bulk-tracked and item-tracked.
func TestLotAndSerialsAreMutuallyExclusive(t *testing.T) {
	quantity, err := NewQuantity(2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewGoodsReceiptLine(uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		quantity, "", "LOT-1", []string{"SN-1", "SN-2"}, nil, "")
	if err == nil {
		t.Error("a line naming both a lot and serials was accepted")
	}
}

func TestSerialCountMustMatchQuantity(t *testing.T) {
	quantity, err := NewQuantity(3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewGoodsReceiptLine(uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		quantity, "", "", []string{"SN-1", "SN-2"}, nil, ""); err == nil {
		t.Error("two serials were accepted for a three-unit line")
	}
	line, err := NewGoodsReceiptLine(uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		quantity, "", "", []string{"SN-1", "SN-2", "SN-3"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !line.IsSerialTracked() || line.IsLotTracked() {
		t.Error("line tracking flags wrong")
	}
}

func TestLineRejectsMissingIdentifiers(t *testing.T) {
	quantity, err := NewQuantity(1)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func() error{
		"nil product": func() error {
			_, err := NewGoodsReceiptLine(uuid.New(), uuid.Nil, uuid.New(), uuid.New(), quantity, "", "", nil, nil, "")
			return err
		},
		"nil location": func() error {
			_, err := NewGoodsReceiptLine(uuid.New(), uuid.New(), uuid.Nil, uuid.New(), quantity, "", "", nil, nil, "")
			return err
		},
		"nil uom": func() error {
			_, err := NewGoodsReceiptLine(uuid.New(), uuid.New(), uuid.New(), uuid.Nil, quantity, "", "", nil, nil, "")
			return err
		},
	}
	for label, fn := range cases {
		if err := fn(); err == nil {
			t.Errorf("%s: expected rejection", label)
		}
	}
}

func TestSerialNumbersAreDefensivelyCopied(t *testing.T) {
	quantity, err := NewQuantity(2)
	if err != nil {
		t.Fatal(err)
	}
	supplied := []string{"SN-1", "SN-2"}
	line, err := NewGoodsReceiptLine(uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		quantity, "", "", supplied, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	supplied[0] = "TAMPERED"
	if line.SerialNumbers()[0] != "SN-1" {
		t.Fatal("mutating the supplied slice changed the line")
	}
	returned := line.SerialNumbers()
	returned[0] = "TAMPERED"
	if line.SerialNumbers()[0] != "SN-1" {
		t.Fatal("mutating the returned slice changed the line")
	}
}
