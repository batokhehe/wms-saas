package entity

import (
	"github.com/google/uuid"
	"testing"
	"time"
)

func testProduct(t *testing.T) *Product {
	sku, _ := NewSKU("SKU-1")
	name, _ := NewProductName("Widget")
	p, e := NewProduct(uuid.New(), uuid.New(), sku, name, "", uuid.New(), uuid.New(), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestSetPrimaryBarcodeIsAtomic(t *testing.T) {
	p := testProduct(t)
	a, _ := NewBarcode("A")
	b, _ := NewBarcode("B")
	actor := uuid.New()
	_ = p.AddBarcode(a, true, actor, time.Now())
	_ = p.AddBarcode(b, false, actor, time.Now())
	x, _ := NewBarcode("X")
	if p.SetPrimaryBarcode(x, actor, time.Now()) == nil {
		t.Fatal("expected error")
	}
	if !p.Barcodes()[0].IsPrimary() {
		t.Fatal("failed command mutated aggregate")
	}
}
func TestDecimalIsExact(t *testing.T) {
	v, e := NewConversionFactor("0.1")
	if e != nil || v.Decimal().String() != "1/10" {
		t.Fatalf("not exact: %v %v", v, e)
	}
}
func TestDiscontinuedIsTerminal(t *testing.T) {
	p := testProduct(t)
	actor := uuid.New()
	_ = p.Discontinue(actor, time.Now())
	if p.Activate(actor, time.Now()) == nil {
		t.Fatal("terminal state violated")
	}
}
