package entity

import "testing"

func TestQuantitiesRejectNegative(t *testing.T) {
	if _, err := NewInventoryQuantity(-1); err == nil {
		t.Error("negative on-hand accepted")
	}
	if _, err := NewReservedQuantity(-1); err == nil {
		t.Error("negative reserved accepted")
	}
	if _, err := NewInventoryQuantity(0); err != nil {
		t.Error("zero on-hand should be valid")
	}
}

func TestQuantityMustBePositive(t *testing.T) {
	if _, err := NewQuantity(1); err != nil {
		t.Error("positive movement rejected")
	}
	for _, v := range []int64{0, -1} {
		if _, err := NewQuantity(v); err == nil {
			t.Errorf("NewQuantity(%d) accepted", v)
		}
	}
}

func TestAvailableDerivation(t *testing.T) {
	on := MustInventoryQuantity(10)
	res, _ := NewReservedQuantity(4)
	if got := available(on, res); got.Value() != 6 {
		t.Errorf("available = %d, want 6", got.Value())
	}
}

func TestTrackingTypeRules(t *testing.T) {
	if !TrackingNone.Valid() || !TrackingLot.Valid() || !TrackingSerial.Valid() {
		t.Error("a known tracking type reported invalid")
	}
	if TrackingType("X").Valid() {
		t.Error("an unknown tracking type reported valid")
	}
	if !TrackingLot.RequiresLot() || TrackingLot.RequiresSerial() {
		t.Error("LOT tracking rules wrong")
	}
	if !TrackingSerial.RequiresSerial() || TrackingSerial.RequiresLot() {
		t.Error("SERIAL tracking rules wrong")
	}
}

func TestInventoryStatusValidity(t *testing.T) {
	if !StatusActive.Valid() || !StatusLocked.Valid() {
		t.Error("a known status reported invalid")
	}
	if InventoryStatus("X").Valid() {
		t.Error("an unknown status reported valid")
	}
}

func TestLotAndSerialValidation(t *testing.T) {
	if _, err := NewLotNumber("  "); err == nil {
		t.Error("blank lot accepted")
	}
	if _, err := NewLotNumber("LOT-1"); err != nil {
		t.Error("valid lot rejected")
	}
	if NoLotNumber().IsZero() != true {
		t.Error("NoLotNumber should be zero")
	}
	if _, err := NewSerialNumber(""); err == nil {
		t.Error("blank serial accepted")
	}
	if s, _ := NewSerialNumber("  SN-9  "); s.String() != "SN-9" {
		t.Errorf("serial not trimmed: %q", s.String())
	}
}

func TestOverflowGuard(t *testing.T) {
	// Near the int64 ceiling, adding a movement must be refused, not wrapped.
	huge := MustInventoryQuantity(9223372036854775806) // MaxInt64 - 1
	if _, err := huge.add(MustQuantity(10)); err == nil {
		t.Error("overflow was not guarded")
	}
}
