// Package entity holds the base type every domain entity embeds.
//
// It is the one place in the codebase where a struct definition is shared
// across modules. That is deliberate: identity and lifecycle timestamps are not
// a business concern, and letting each module define its own would guarantee
// that some of them get soft deletes subtly wrong.
package entity

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BaseEntity is embedded by every persisted domain entity.
//
//	type Product struct {
//	    entity.BaseEntity
//	    CompanyID uuid.UUID
//	    SKU       string
//	}
//
// # Why gorm is imported here
//
// The project rule is that `gorm` appears only inside a module's repository/
// package. This type is the single, documented exception, and it is narrow:
// gorm.DeletedAt is a *data* type, not behaviour. No *gorm.DB, no queries, no
// session handling ever appears in an entity.
//
// The exception buys something specific. gorm.DeletedAt makes soft deletion
// automatic: GORM appends `WHERE deleted_at IS NULL` to every query against the
// model without the caller asking. Hand-rolling that with a *time.Time means
// every single query must remember the filter, and the first one that forgets
// silently resurrects deleted rows — in a WMS, that is deleted stock reappearing
// in an availability check.
type BaseEntity struct {
	// ID is assigned by the repository from port.IDGenerator, never by
	// uuid.New() at the call site and never by the database.
	//
	// Client-assigned UUIDs matter here because the Flutter app works offline:
	// a device that cannot mint an identifier must round-trip to the server
	// before it can reference anything it just created.
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	// Version is the persistence-owned optimistic-lock token. Repositories use
	// it in conditional updates and are the only layer allowed to advance it.
	Version uint64 `gorm:"not null;default:1" json:"-"`

	// CreatedAt and UpdatedAt are managed by GORM. See the note below.
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`

	// DeletedAt is the soft-delete marker. It is json:"-" because deletion
	// state is not part of any API contract: an endpoint either returns a row
	// or it does not.
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// Compile-time proof that BaseEntity satisfies the repository's constraint.
// If a field is renamed, this breaks here rather than at every call site.
var _ Identifiable = (*BaseEntity)(nil)

// Identifiable is the contract the generic base repository requires.
//
// It exists so repository.Base can assign an ID to a value of an unknown type
// parameter. Without it, the generic repository could not generate identifiers
// and every module would be back to calling uuid.New() by hand.
type Identifiable interface {
	GetID() uuid.UUID
	SetID(id uuid.UUID)
	IsPersisted() bool
	GetVersion() uint64
	SetVersion(version uint64)
}

// GetID returns the entity's identifier.
func (b *BaseEntity) GetID() uuid.UUID { return b.ID }

// SetID assigns the entity's identifier.
func (b *BaseEntity) SetID(id uuid.UUID) { b.ID = id }

// IsPersisted reports whether an identifier has been assigned. The repository
// uses it to decide whether to generate one on create.
func (b *BaseEntity) IsPersisted() bool { return b.ID != uuid.Nil }

// GetVersion returns the optimistic-lock token stored with the row.
func (b *BaseEntity) GetVersion() uint64 { return b.Version }

// SetVersion is intentionally part of the persistence contract. Domain
// aggregates expose no equivalent mutator; repositories alone advance it.
func (b *BaseEntity) SetVersion(version uint64) { b.Version = version }

// IsDeleted reports whether the entity has been soft-deleted.
//
// This is rarely needed: GORM filters deleted rows out automatically, so a
// value loaded through a normal query is never deleted. It matters only when a
// query deliberately opted in with repository.WithDeleted.
func (b *BaseEntity) IsDeleted() bool { return b.DeletedAt.Valid }

// DeletedAtTime returns the deletion timestamp, or the zero time if the entity
// is live. It keeps `.DeletedAt.Time` / `.DeletedAt.Valid` unpacking out of
// service code.
func (b *BaseEntity) DeletedAtTime() time.Time {
	if !b.DeletedAt.Valid {
		return time.Time{}
	}
	return b.DeletedAt.Time
}

// # A note on CreatedAt and UpdatedAt
//
// These are managed by GORM, not by the injected Clock. This is a deliberate
// decision rather than an oversight.
//
// GORM overwrites UpdatedAt on every write regardless of what the caller set,
// so a repository that assigned it from a Clock would be silently overridden —
// producing code that looks injectable but is not. Fighting the ORM to reclaim
// two audit columns is not worth the complexity.
//
// port.Clock is therefore for *business* time — expiry windows, scheduling,
// cut-off calculations — which is what actually needs to be deterministic in a
// unit test. Row audit timestamps do not.
//
// When a test genuinely needs a fixed CreatedAt, set it explicitly before
// Create: GORM preserves a non-zero CreatedAt.
