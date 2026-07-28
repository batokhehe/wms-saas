package entity

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// --- test builders --------------------------------------------------------

// build returns a fresh DRAFT product plus the ids a test needs to exercise
// the aggregate: the actor and the base UOM id. testProduct (product_test.go)
// hides both, so the invariant suite constructs its own.
func build(t *testing.T) (p *Product, baseUOM, actor uuid.UUID) {
	t.Helper()
	sku, err := NewSKU("SKU-1")
	if err != nil {
		t.Fatal(err)
	}
	name, err := NewProductName("Widget")
	if err != nil {
		t.Fatal(err)
	}
	baseUOM = uuid.New()
	actor = uuid.New()
	p, err = NewProduct(uuid.New(), uuid.New(), sku, name, "", baseUOM, actor, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p.PullEvents() // discard the creation event so per-test assertions start clean
	return p, baseUOM, actor
}

func mustBarcode(t *testing.T, raw string) Barcode {
	t.Helper()
	b, err := NewBarcode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func lastEvent(t *testing.T, p *Product) Event {
	t.Helper()
	evs := p.PullEvents()
	if len(evs) == 0 {
		t.Fatal("expected an event, got none")
	}
	return evs[len(evs)-1]
}

func ptrWeight(t *testing.T, raw string) *Weight {
	t.Helper()
	w, err := NewWeightKilograms(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &w
}

func ptrVolume(t *testing.T, raw string) *Volume {
	t.Helper()
	v, err := NewVolumeCubicMetres(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &v
}

func ptrDimension(t *testing.T, w, h, l string) *Dimension {
	t.Helper()
	lw, err := NewLengthCentimetres(w)
	if err != nil {
		t.Fatal(err)
	}
	lh, err := NewLengthCentimetres(h)
	if err != nil {
		t.Fatal(err)
	}
	ll, err := NewLengthCentimetres(l)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDimension(lw, lh, ll)
	if err != nil {
		t.Fatal(err)
	}
	return &d
}

// --- construction ---------------------------------------------------------

func TestNewProductStartsInSafeDefaultState(t *testing.T) {
	p, baseUOM, _ := build(t)
	if p.Status() != StatusDraft {
		t.Fatalf("want DRAFT, got %s", p.Status())
	}
	if p.TrackingMethod() != TrackingNone {
		t.Fatalf("want NONE tracking, got %s", p.TrackingMethod())
	}
	if p.Version() != 1 {
		t.Fatalf("want version 1, got %d", p.Version())
	}
	uoms := p.UOMs()
	if len(uoms) != 1 || uoms[0].UOMID() != baseUOM {
		t.Fatalf("base uom not provisioned: %+v", uoms)
	}
	if uoms[0].ConversionFactor().Decimal().String() != "1" {
		t.Fatalf("base factor must be 1, got %s", uoms[0].ConversionFactor().Decimal().String())
	}
}

func TestNewProductRejectsNilIdentifiers(t *testing.T) {
	sku, _ := NewSKU("SKU-1")
	name, _ := NewProductName("Widget")
	cases := map[string]struct{ id, company, base, actor uuid.UUID }{
		"nil id":      {uuid.Nil, uuid.New(), uuid.New(), uuid.New()},
		"nil company": {uuid.New(), uuid.Nil, uuid.New(), uuid.New()},
		"nil base":    {uuid.New(), uuid.New(), uuid.Nil, uuid.New()},
		"nil actor":   {uuid.New(), uuid.New(), uuid.New(), uuid.Nil},
	}
	for label, c := range cases {
		if _, err := NewProduct(c.id, c.company, sku, name, "", c.base, c.actor, time.Now()); err == nil {
			t.Fatalf("%s: expected rejection", label)
		}
	}
}

func TestNewProductEmitsEnrichedCreationEvent(t *testing.T) {
	sku, _ := NewSKU("SKU-9")
	name, _ := NewProductName("Gadget")
	base := uuid.New()
	p, err := NewProduct(uuid.New(), uuid.New(), sku, name, "", base, uuid.New(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ev := lastEvent(t, p)
	if ev.Name != EventProductCreated {
		t.Fatalf("want created event, got %s", ev.Name)
	}
	for _, key := range []string{"sku", "name", "base_uom_id", "status", "tracking"} {
		if _, ok := ev.Attributes[key]; !ok {
			t.Fatalf("creation event missing %q: %+v", key, ev.Attributes)
		}
	}
	if ev.Attributes["base_uom_id"] != base.String() {
		t.Fatal("base_uom_id fact is not the actual base uom")
	}
}

// --- reconstitution -------------------------------------------------------

func TestReconstituteRaisesNoEvents(t *testing.T) {
	base := uuid.New()
	sku, _ := NewSKU("SKU-1")
	name, _ := NewProductName("Widget")
	p, err := Reconstitute(uuid.New(), uuid.New(), 7, sku, name, "", nil, nil, base,
		StatusActive, TrackingLot, NoShelfLife(), nil, nil, nil, nil,
		[]ProductUOM{{base, MustConversionFactor("1")}}, uuid.New(), uuid.New(), time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if evs := p.PullEvents(); len(evs) != 0 {
		t.Fatalf("reconstitution must not raise events, got %d", len(evs))
	}
	if p.Version() != 7 {
		t.Fatalf("want restored version 7, got %d", p.Version())
	}
}

func TestReconstituteRejectsInvalidPersistedState(t *testing.T) {
	base := uuid.New()
	sku, _ := NewSKU("SKU-1")
	name, _ := NewProductName("Widget")
	good := []ProductUOM{{base, MustConversionFactor("1")}}
	twoPrimary := []ProductBarcode{{mustBarcode(t, "A"), true}, {mustBarcode(t, "B"), true}}
	zeroFactor := []ProductUOM{{base, MustConversionFactor("1")}, {uuid.New(), ConversionFactor{}}}
	noBase := []ProductUOM{{uuid.New(), MustConversionFactor("2")}}

	cases := map[string]func() (*Product, error){
		"version zero": func() (*Product, error) {
			return Reconstitute(uuid.New(), uuid.New(), 0, sku, name, "", nil, nil, base, StatusDraft, TrackingNone, NoShelfLife(), nil, nil, nil, nil, good, uuid.New(), uuid.New(), time.Now(), time.Now())
		},
		"invalid status": func() (*Product, error) {
			return Reconstitute(uuid.New(), uuid.New(), 1, sku, name, "", nil, nil, base, Status("BOGUS"), TrackingNone, NoShelfLife(), nil, nil, nil, nil, good, uuid.New(), uuid.New(), time.Now(), time.Now())
		},
		"invalid tracking": func() (*Product, error) {
			return Reconstitute(uuid.New(), uuid.New(), 1, sku, name, "", nil, nil, base, StatusDraft, TrackingMethod("BOGUS"), NoShelfLife(), nil, nil, nil, nil, good, uuid.New(), uuid.New(), time.Now(), time.Now())
		},
		"missing base uom": func() (*Product, error) {
			return Reconstitute(uuid.New(), uuid.New(), 1, sku, name, "", nil, nil, base, StatusDraft, TrackingNone, NoShelfLife(), nil, nil, nil, nil, noBase, uuid.New(), uuid.New(), time.Now(), time.Now())
		},
		"two primary barcodes": func() (*Product, error) {
			return Reconstitute(uuid.New(), uuid.New(), 1, sku, name, "", nil, nil, base, StatusDraft, TrackingNone, NoShelfLife(), nil, nil, nil, twoPrimary, good, uuid.New(), uuid.New(), time.Now(), time.Now())
		},
		"zero-factor uom": func() (*Product, error) {
			return Reconstitute(uuid.New(), uuid.New(), 1, sku, name, "", nil, nil, base, StatusDraft, TrackingNone, NoShelfLife(), nil, nil, nil, nil, zeroFactor, uuid.New(), uuid.New(), time.Now(), time.Now())
		},
	}
	for label, fn := range cases {
		if _, err := fn(); err == nil {
			t.Fatalf("%s: expected reconstitution to reject", label)
		}
	}
}

// --- barcodes: exactly one primary ---------------------------------------

func TestFirstBarcodeIsForcedPrimary(t *testing.T) {
	p, _, actor := build(t)
	if err := p.AddBarcode(mustBarcode(t, "A"), false, actor, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !p.Barcodes()[0].IsPrimary() {
		t.Fatal("first barcode must be promoted to primary regardless of the flag")
	}
	ev := lastEvent(t, p)
	if ev.Name != EventBarcodeAdded || ev.Attributes["barcode"] != "A" || ev.Attributes["primary"] != true {
		t.Fatalf("barcode-added event lacks barcode+primary facts: %+v", ev.Attributes)
	}
}

func TestAddingPrimaryDemotesPrevious(t *testing.T) {
	p, _, actor := build(t)
	_ = p.AddBarcode(mustBarcode(t, "A"), true, actor, time.Now())
	_ = p.AddBarcode(mustBarcode(t, "B"), true, actor, time.Now())
	primaries := 0
	for _, b := range p.Barcodes() {
		if b.IsPrimary() {
			primaries++
		}
	}
	if primaries != 1 {
		t.Fatalf("want exactly one primary, got %d", primaries)
	}
	if !p.Barcodes()[1].IsPrimary() {
		t.Fatal("newest primary should win")
	}
}

func TestDuplicateBarcodeRejected(t *testing.T) {
	p, _, actor := build(t)
	_ = p.AddBarcode(mustBarcode(t, "A"), true, actor, time.Now())
	if err := p.AddBarcode(mustBarcode(t, "A"), false, actor, time.Now()); err == nil {
		t.Fatal("expected duplicate barcode to be rejected")
	}
}

func TestRemovePrimaryBlockedWhileOthersExist(t *testing.T) {
	p, _, actor := build(t)
	_ = p.AddBarcode(mustBarcode(t, "A"), true, actor, time.Now())
	_ = p.AddBarcode(mustBarcode(t, "B"), false, actor, time.Now())
	if err := p.RemoveBarcode(mustBarcode(t, "A"), actor, time.Now()); err == nil {
		t.Fatal("removing the sole primary while others remain must be blocked")
	}
	// invariant still holds: one primary present
	if !p.Barcodes()[0].IsPrimary() {
		t.Fatal("blocked removal must not have mutated the set")
	}
}

func TestRemoveLastPrimaryBarcodeAllowed(t *testing.T) {
	p, _, actor := build(t)
	_ = p.AddBarcode(mustBarcode(t, "A"), true, actor, time.Now())
	if err := p.RemoveBarcode(mustBarcode(t, "A"), actor, time.Now()); err != nil {
		t.Fatalf("removing the last barcode should be allowed: %v", err)
	}
	if len(p.Barcodes()) != 0 {
		t.Fatal("expected empty barcode set")
	}
	ev := lastEvent(t, p)
	if ev.Name != EventBarcodeRemoved || ev.Attributes["primary"] != true {
		t.Fatalf("removed event must carry the primary fact: %+v", ev.Attributes)
	}
}

func TestRemoveUnknownBarcodeIsNotFound(t *testing.T) {
	p, _, actor := build(t)
	if err := p.RemoveBarcode(mustBarcode(t, "Z"), actor, time.Now()); err == nil {
		t.Fatal("expected not-found for unknown barcode")
	}
}

func TestSetPrimaryBarcodeSuccessIsExclusive(t *testing.T) {
	p, _, actor := build(t)
	_ = p.AddBarcode(mustBarcode(t, "A"), true, actor, time.Now())
	_ = p.AddBarcode(mustBarcode(t, "B"), false, actor, time.Now())
	if err := p.SetPrimaryBarcode(mustBarcode(t, "B"), actor, time.Now()); err != nil {
		t.Fatal(err)
	}
	primaries := 0
	for _, b := range p.Barcodes() {
		if b.IsPrimary() {
			primaries++
		}
	}
	if primaries != 1 || !p.Barcodes()[1].IsPrimary() {
		t.Fatalf("set-primary must leave exactly one primary on the target: %+v", p.Barcodes())
	}
	ev := lastEvent(t, p)
	if ev.Attributes["old"] != "A" || ev.Attributes["new"] != "B" {
		t.Fatalf("primary-changed event lacks old/new facts: %+v", ev.Attributes)
	}
}

// --- uom: base cannot disappear, factors exact ---------------------------

func TestBaseUOMCannotBeRemoved(t *testing.T) {
	p, base, actor := build(t)
	if err := p.RemoveUOM(base, actor, time.Now()); err == nil {
		t.Fatal("base uom removal must be blocked")
	}
}

func TestAddUOMRejectsUnconstructedFactor(t *testing.T) {
	p, _, actor := build(t)
	before := len(p.UOMs())
	if err := p.AddUOM(uuid.New(), ConversionFactor{}, actor, time.Now()); err == nil {
		t.Fatal("zero-value conversion factor must be rejected")
	}
	if len(p.UOMs()) != before {
		t.Fatal("rejected AddUOM must not mutate the set")
	}
	if evs := p.PullEvents(); len(evs) != 0 {
		t.Fatal("rejected AddUOM must not emit an event")
	}
}

func TestAddUOMRejectsReAddingBase(t *testing.T) {
	p, base, actor := build(t)
	if err := p.AddUOM(base, MustConversionFactor("1"), actor, time.Now()); err == nil {
		t.Fatal("re-adding the base uom must be rejected")
	}
}

func TestAddUOMRejectsDuplicate(t *testing.T) {
	p, _, actor := build(t)
	id := uuid.New()
	_ = p.AddUOM(id, MustConversionFactor("12"), actor, time.Now())
	if err := p.AddUOM(id, MustConversionFactor("6"), actor, time.Now()); err == nil {
		t.Fatal("duplicate uom must be rejected")
	}
}

func TestAddUOMStoresExactFactorAndEmitsFact(t *testing.T) {
	p, _, actor := build(t)
	id := uuid.New()
	if err := p.AddUOM(id, MustConversionFactor("0.333"), actor, time.Now()); err != nil {
		t.Fatal(err)
	}
	var stored ProductUOM
	for _, u := range p.UOMs() {
		if u.UOMID() == id {
			stored = u
		}
	}
	if stored.ConversionFactor().Decimal().String() != "333/1000" {
		t.Fatalf("factor not stored as exact rational: %s", stored.ConversionFactor().Decimal().String())
	}
	ev := lastEvent(t, p)
	if ev.Name != EventUOMAdded || ev.Attributes["factor"] != "333/1000" {
		t.Fatalf("uom-added event lacks exact factor: %+v", ev.Attributes)
	}
}

func TestRemoveUOMEmitsFactAndRejectsUnknown(t *testing.T) {
	p, _, actor := build(t)
	id := uuid.New()
	_ = p.AddUOM(id, MustConversionFactor("2"), actor, time.Now())
	p.PullEvents()
	if err := p.RemoveUOM(id, actor, time.Now()); err != nil {
		t.Fatal(err)
	}
	ev := lastEvent(t, p)
	if ev.Name != EventUOMRemoved || ev.Attributes["uom_id"] != id.String() {
		t.Fatalf("removed event lacks uom id: %+v", ev.Attributes)
	}
	if err := p.RemoveUOM(uuid.New(), actor, time.Now()); err == nil {
		t.Fatal("removing unknown uom must be not-found")
	}
}

// --- measurements: validation, NaN/Inf, no partial mutation --------------

func TestMeasurementConstructorsRejectNaNAndInf(t *testing.T) {
	for _, raw := range []string{"NaN", "Inf", "-Inf", "+Inf", "nan", "infinity", "abc", ""} {
		if _, err := NewWeightKilograms(raw); err == nil {
			t.Fatalf("weight accepted non-finite input %q", raw)
		}
		if _, err := NewVolumeCubicMetres(raw); err == nil {
			t.Fatalf("volume accepted non-finite input %q", raw)
		}
		if _, err := NewLengthCentimetres(raw); err == nil {
			t.Fatalf("length accepted non-finite input %q", raw)
		}
	}
}

func TestSetMeasurementsRejectsUnconstructedValues(t *testing.T) {
	p, _, actor := build(t)
	zeroW := &Weight{}
	if err := p.SetMeasurements(zeroW, nil, nil, actor, time.Now()); err == nil {
		t.Fatal("zero-value weight must be rejected")
	}
	zeroV := &Volume{}
	if err := p.SetMeasurements(nil, nil, zeroV, actor, time.Now()); err == nil {
		t.Fatal("zero-value volume must be rejected")
	}
	zeroD := &Dimension{}
	if err := p.SetMeasurements(nil, zeroD, nil, actor, time.Now()); err == nil {
		t.Fatal("zero-value dimension must be rejected")
	}
}

func TestSetMeasurementsIsAtomicOnError(t *testing.T) {
	p, _, actor := build(t)
	// seed a valid profile first
	if err := p.SetMeasurements(ptrWeight(t, "2"), ptrDimension(t, "1", "1", "1"), ptrVolume(t, "3"), actor, time.Now()); err != nil {
		t.Fatal(err)
	}
	p.PullEvents()
	// now a call with one invalid side must change nothing
	if err := p.SetMeasurements(ptrWeight(t, "9"), &Dimension{}, ptrVolume(t, "9"), actor, time.Now()); err == nil {
		t.Fatal("expected rejection from invalid dimension")
	}
	if p.Weight().Kilograms().String() != "2" || p.Volume().CubicMetres().String() != "3" {
		t.Fatal("a failed SetMeasurements left a partial mutation")
	}
	if evs := p.PullEvents(); len(evs) != 0 {
		t.Fatal("a failed SetMeasurements must not emit an event")
	}
}

func TestSetMeasurementsClearsWithNil(t *testing.T) {
	p, _, actor := build(t)
	_ = p.SetMeasurements(ptrWeight(t, "2"), ptrDimension(t, "1", "1", "1"), ptrVolume(t, "3"), actor, time.Now())
	if err := p.SetMeasurements(nil, nil, nil, actor, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.Weight() != nil || p.Dimension() != nil || p.Volume() != nil {
		t.Fatal("nil measurements should clear the physical profile")
	}
}

func TestNewDimensionRejectsUnconstructedSide(t *testing.T) {
	l, _ := NewLengthCentimetres("5")
	if _, err := NewDimension(l, l, Length{}); err == nil {
		t.Fatal("dimension with a zero-value side must be rejected")
	}
	if _, err := NewDimension(l, l, l); err != nil {
		t.Fatalf("valid dimension rejected: %v", err)
	}
}

// --- shelf life -----------------------------------------------------------

func TestShelfLifeDistinguishesUndefinedFromZeroDays(t *testing.T) {
	if NoShelfLife().IsDefined() {
		t.Fatal("NoShelfLife must be undefined")
	}
	zero, err := NewShelfLife(0)
	if err != nil {
		t.Fatal(err)
	}
	if !zero.IsDefined() || zero.Days() != 0 {
		t.Fatal("a zero-day shelf life is defined and valid")
	}
	if _, err := NewShelfLife(-1); err == nil {
		t.Fatal("negative shelf life must be rejected")
	}
}

func TestSetShelfLifeEmitsChangeAndSkipsNoop(t *testing.T) {
	p, _, actor := build(t)
	defined, _ := NewShelfLife(30)
	p.SetShelfLife(defined, actor, time.Now())
	ev := lastEvent(t, p)
	if ev.Name != EventShelfLifeChanged || ev.Attributes["new_defined"] != true || ev.Attributes["new_days"] != 30 {
		t.Fatalf("shelf-life event lacks new facts: %+v", ev.Attributes)
	}
	// idempotent set emits nothing
	p.SetShelfLife(defined, actor, time.Now())
	if evs := p.PullEvents(); len(evs) != 0 {
		t.Fatal("no-op shelf life change must not emit an event")
	}
}

// --- tracking transitions -------------------------------------------------

func TestTrackingBlockedWhileInventoryExists(t *testing.T) {
	p, _, actor := build(t)
	if err := p.EnableLotTracking(true, actor, time.Now()); err == nil {
		t.Fatal("tracking change must be blocked while inventory exists")
	}
	if p.TrackingMethod() != TrackingNone {
		t.Fatal("blocked tracking change must not mutate the aggregate")
	}
}

func TestTrackingSameMethodWithInventoryIsNoop(t *testing.T) {
	p, _, actor := build(t)
	// already NONE; disabling with inventory present is a no-op, not a conflict
	if err := p.DisableTracking(true, actor, time.Now()); err != nil {
		t.Fatalf("no-op tracking set must be allowed even with inventory: %v", err)
	}
}

func TestTrackingTransitionEmitsOldAndNew(t *testing.T) {
	p, _, actor := build(t)
	if err := p.EnableSerialTracking(false, actor, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.TrackingMethod() != TrackingSerial {
		t.Fatalf("want SERIAL, got %s", p.TrackingMethod())
	}
	ev := lastEvent(t, p)
	if ev.Name != EventTrackingMethodChanged || ev.Attributes["old"] != "NONE" || ev.Attributes["new"] != "SERIAL" {
		t.Fatalf("tracking event must carry old and new method: %+v", ev.Attributes)
	}
}

func TestInvalidTrackingMethodRejected(t *testing.T) {
	p, _, actor := build(t)
	if err := p.SetTracking(TrackingMethod("BOGUS"), false, actor, time.Now()); err == nil {
		t.Fatal("invalid tracking method must be rejected")
	}
}

// --- lifecycle: discontinued is terminal ---------------------------------

func TestDiscontinueBlocksAllTransitions(t *testing.T) {
	p, _, actor := build(t)
	if err := p.Discontinue(actor, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.Activate(actor, time.Now()) == nil {
		t.Fatal("discontinued cannot activate")
	}
	if p.Deactivate(actor, time.Now()) == nil {
		t.Fatal("discontinued cannot deactivate")
	}
	if p.Discontinue(actor, time.Now()) == nil {
		t.Fatal("second discontinue must conflict")
	}
}

func TestRenameBlockedWhileActive(t *testing.T) {
	p, _, actor := build(t)
	_ = p.Activate(actor, time.Now())
	p.PullEvents()
	newName, _ := NewProductName("Renamed")
	if err := p.Rename(newName, actor, time.Now()); err == nil {
		t.Fatal("active product rename must be blocked")
	}
	// draft rename works and is a no-op when unchanged
	_ = p.Deactivate(actor, time.Now())
	if err := p.Rename(newName, actor, time.Now()); err != nil {
		t.Fatal(err)
	}
	p.PullEvents()
	if err := p.Rename(newName, actor, time.Now()); err != nil {
		t.Fatal(err)
	}
	if evs := p.PullEvents(); len(evs) != 0 {
		t.Fatal("no-op rename must not emit an event")
	}
}

// --- optimistic locking ---------------------------------------------------

func TestVersionIsNotMutatedByBusinessMethods(t *testing.T) {
	p, _, actor := build(t)
	start := p.Version()
	_ = p.AddBarcode(mustBarcode(t, "A"), true, actor, time.Now())
	_ = p.AddUOM(uuid.New(), MustConversionFactor("5"), actor, time.Now())
	_ = p.SetMeasurements(ptrWeight(t, "1"), nil, nil, actor, time.Now())
	_ = p.EnableLotTracking(false, actor, time.Now())
	_ = p.Activate(actor, time.Now())
	_ = p.Discontinue(actor, time.Now())
	if p.Version() != start {
		t.Fatalf("no business method may change Version; want %d, got %d", start, p.Version())
	}
}
