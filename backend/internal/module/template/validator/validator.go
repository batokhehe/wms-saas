// Package validator holds this module's custom validation rules.
//
// Struct-tag validation (required, min, max, oneof) is declared on the DTOs
// themselves and handled by internal/shared/validator. This package is for the
// rules tags cannot express: cross-field constraints, domain-specific formats,
// and checks that need a lookup.
//
// Rules that need database access do NOT belong here — "does this SKU already
// exist" is a business invariant and lives in the service, where it can be
// enforced inside the same transaction as the write.
package validator

import (
	"regexp"
	"strings"

	"github.com/batokhehe/wms-saas/backend/internal/module/template/dto"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// slugPattern is an example of a module-specific format rule.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ValidateCreate applies rules that binding tags cannot express.
//
// It returns a fully-formed VALIDATION_ERROR with per-field details, so a
// custom rule failure is indistinguishable from a tag failure on the client
// side. That uniformity is why this returns apperror rather than a bare error.
func ValidateCreate(req dto.CreateRequest) error {
	var fields []apperror.FieldError

	// Example: a tag cannot express "must not be only whitespace", because
	// `required` is satisfied by a string of spaces.
	if strings.TrimSpace(req.Name) == "" {
		fields = append(fields, apperror.FieldError{
			Field:   "name",
			Rule:    "not_blank",
			Message: "name must not be blank",
		})
	}

	if len(fields) > 0 {
		return apperror.NewValidation(fields...).WithOp("template.validator.ValidateCreate")
	}

	return nil
}

// IsValidSlug reports whether s is a well-formed slug. Helper predicates stay
// exported so both the service and the validator can share one definition of
// what "valid" means.
func IsValidSlug(s string) bool { return slugPattern.MatchString(s) }
