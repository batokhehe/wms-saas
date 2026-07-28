// Package entity holds the StorageLocation aggregate and its value objects.
//
// LAYER RULE: this package imports NOTHING from the rest of the project except
// pkg/apperror. It does not embed shared/entity.BaseEntity and carries no GORM
// tags — see storage_location.go for why that departure from EntityConvention
// is what makes the aggregate an aggregate.
package entity

import (
	"regexp"
	"strings"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// segmentPattern constrains one coordinate segment.
//
// Deliberately narrow. These values are printed on rack labels, concatenated
// into a location code and read aloud over a radio, so permitting spaces or
// punctuation would mean escaping them correctly in every one of those places
// forever — and a hyphen specifically would collide with the separator the code
// is built from.
var segmentPattern = regexp.MustCompile(`^[A-Z0-9]{1,32}$`)

// coordinateSeparator joins segments into a location code.
const coordinateSeparator = "-"

// Coordinate is the physical address of a location within a warehouse.
//
// # Why it is one value object rather than five string fields
//
// The five parts are meaningless apart. "Rack 3" identifies nothing without an
// aisle, and a bin with no level is not a place anyone can walk to. Modelling
// them as independent fields would permit exactly those half-states — a
// location with a bin but no aisle — which no operator could act on.
//
// Bundling them also gives the pick-path ordering a single home: a picker walks
// aisle by aisle, and the comparison that expresses that belongs with the data
// it compares.
type Coordinate struct {
	zone  string
	aisle string
	rack  string
	level string
	bin   string
}

// NewCoordinate validates and canonicalises a physical address.
//
// Only zone is required. A floor-stack or a receiving dock genuinely has a zone
// and nothing else, and demanding a full five-part coordinate would make those
// locations unrepresentable.
//
// The optional parts must be contiguous from the outside in: a location may
// have zone+aisle+rack, but not zone+rack with the aisle missing — a gap in the
// middle describes no physical place and would sort nonsensically in a pick
// path.
func NewCoordinate(zone, aisle, rack, level, bin string) (Coordinate, error) {
	c := Coordinate{
		zone:  normalizeSegment(zone),
		aisle: normalizeSegment(aisle),
		rack:  normalizeSegment(rack),
		level: normalizeSegment(level),
		bin:   normalizeSegment(bin),
	}

	var fields []apperror.FieldError

	if c.zone == "" {
		fields = append(fields, apperror.FieldError{
			Field: "zone", Rule: "required", Message: "zone is required",
		})
	}

	for _, segment := range []struct {
		name  string
		value string
	}{
		{"zone", c.zone}, {"aisle", c.aisle}, {"rack", c.rack},
		{"level", c.level}, {"bin", c.bin},
	} {
		if segment.value == "" {
			continue
		}
		if !segmentPattern.MatchString(segment.value) {
			fields = append(fields, apperror.FieldError{
				Field: segment.name, Rule: "format",
				Message: segment.name + " must be 1-32 letters or digits",
			})
		}
	}

	// The contiguity rule. Checked as a sequence rather than pairwise so the
	// message names the actual gap.
	ordered := []struct {
		name  string
		value string
	}{
		{"aisle", c.aisle}, {"rack", c.rack}, {"level", c.level}, {"bin", c.bin},
	}
	for i, segment := range ordered {
		if segment.value == "" {
			continue
		}
		for j := 0; j < i; j++ {
			if ordered[j].value == "" {
				fields = append(fields, apperror.FieldError{
					Field: ordered[j].name, Rule: "required",
					Message: ordered[j].name + " is required when " + segment.name + " is supplied",
				})
				break
			}
		}
	}

	if len(fields) > 0 {
		return Coordinate{}, apperror.NewValidation(fields...).
			WithOp("location.NewCoordinate")
	}

	return c, nil
}

// Zone returns the zone segment.
func (c Coordinate) Zone() string { return c.zone }

// Aisle returns the aisle segment, or "".
func (c Coordinate) Aisle() string { return c.aisle }

// Rack returns the rack segment, or "".
func (c Coordinate) Rack() string { return c.rack }

// Level returns the level segment, or "".
func (c Coordinate) Level() string { return c.level }

// Bin returns the bin segment, or "".
func (c Coordinate) Bin() string { return c.bin }

// Segments returns the non-empty segments, outermost first.
func (c Coordinate) Segments() []string {
	segments := make([]string, 0, 5)
	for _, value := range []string{c.zone, c.aisle, c.rack, c.level, c.bin} {
		if value == "" {
			break
		}
		segments = append(segments, value)
	}
	return segments
}

// String renders the coordinate as a hyphen-joined code: "A-01-02-03".
//
// This is what DeriveCode uses, so a location's default code IS its physical
// address. That correspondence is the point: an operator reading "A-01-02-03"
// off a label knows exactly where to walk, with nothing to look up.
func (c Coordinate) String() string {
	return strings.Join(c.Segments(), coordinateSeparator)
}

// Depth reports how many segments are populated.
//
// A depth of 1 is a zone-level location (a dock, a floor stack); 5 is a fully
// specified bin. Future putaway logic will use it to prefer the most specific
// available place.
func (c Coordinate) Depth() int { return len(c.Segments()) }

// IsZero reports whether the coordinate is unset.
func (c Coordinate) IsZero() bool { return c.zone == "" }

// normalizeSegment canonicalises one segment.
//
// Upper-cased because the columns are plain VARCHAR rather than CITEXT — the
// coordinate parts are sorted in pick-path queries, and a case-insensitive
// collation would make that ordering locale-dependent.
func normalizeSegment(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}
