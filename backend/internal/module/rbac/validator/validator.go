// Package validator holds the RBAC module's custom validation rules.
//
// Struct-tag validation (required, max, dive) is declared on the DTOs and
// handled by internal/shared/validator. This package holds the rules tags
// cannot express.
package validator

import (
	"regexp"
	"strings"

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// roleNamePattern constrains a role name to letters, digits and underscores.
//
// Deliberately narrow, because this value is the join key against
// memberships.role and appears in role pickers and audit lines. Permitting
// spaces or punctuation would mean normalising it identically in both modules
// forever, and a mismatch would silently resolve to no permissions.
var roleNamePattern = regexp.MustCompile(`^[A-Z0-9_]{2,32}$`)

// ValidateCreateRole applies the rules binding tags cannot express.
//
// Every violation is reported rather than stopping at the first, so a form does
// not reject a submission once per problem.
func ValidateCreateRole(req dto.CreateRoleRequest) error {
	var fields []apperror.FieldError

	name := entity.NormalizeRoleName(req.Name)

	if !roleNamePattern.MatchString(name) {
		fields = append(fields, apperror.FieldError{
			Field:   "name",
			Rule:    "format",
			Message: "name must be 2-32 letters, digits or underscores",
		})
	}

	// A caller must not be able to create a role called OWNER, ADMIN or STAFF.
	// The unique index would reject it anyway once the company is provisioned,
	// but that produces a bare CONFLICT; this explains WHY, and it also blocks
	// the race where a company has not yet been provisioned and the name would
	// otherwise be claimed by a non-system role that memberships then resolve
	// against.
	if entity.IsSystemRoleName(name) {
		fields = append(fields, apperror.FieldError{
			Field:   "name",
			Rule:    "reserved",
			Message: "OWNER, ADMIN and STAFF are system roles and cannot be redefined",
		})
	}

	if err := validatePermissionCodes(req.Permissions, &fields); err != nil {
		return err
	}

	if len(fields) > 0 {
		return apperror.NewValidation(fields...).
			WithOp("rbac.validator.ValidateCreateRole")
	}

	return nil
}

// ValidateUpdateRole checks a partial update.
func ValidateUpdateRole(req dto.UpdateRoleRequest) error {
	// Description is the only mutable field and its length is already bounded
	// by the binding tag. A blank description is legitimate — it means "no
	// description" — so unlike a name there is nothing to reject here.
	//
	// The function exists anyway so that adding a mutable field later has an
	// obvious home, rather than the first new rule being written inline in the
	// service.
	_ = req
	return nil
}

// ValidateSetPermissions checks a permission replacement request.
func ValidateSetPermissions(req dto.SetRolePermissionsRequest) error {
	var fields []apperror.FieldError

	if err := validatePermissionCodes(req.Permissions, &fields); err != nil {
		return err
	}

	if len(fields) > 0 {
		return apperror.NewValidation(fields...).
			WithOp("rbac.validator.ValidateSetPermissions")
	}

	return nil
}

// validatePermissionCodes rejects unknown codes and duplicates.
//
// Checking against the in-code catalogue rather than the database is
// deliberate: an unknown code is a client error that deserves an immediate 422
// naming the offending value, not a foreign-key violation translated into a
// generic conflict. The database constraint remains the backstop.
func validatePermissionCodes(codes []string, fields *[]apperror.FieldError) error {
	known := make(map[entity.Code]struct{}, len(entity.PermissionCatalogue()))
	for _, code := range entity.PermissionCatalogue() {
		known[code] = struct{}{}
	}

	seen := make(map[entity.Code]struct{}, len(codes))

	for _, raw := range codes {
		code := entity.NormalizeCode(raw)

		if _, ok := known[code]; !ok {
			*fields = append(*fields, apperror.FieldError{
				Field:   "permissions",
				Rule:    "unknown",
				Message: "unknown permission code: " + strings.TrimSpace(raw),
			})
			continue
		}

		// A duplicate is a client bug rather than an attack, but silently
		// deduplicating would make the request and the stored result differ
		// without the caller knowing.
		if _, duplicate := seen[code]; duplicate {
			*fields = append(*fields, apperror.FieldError{
				Field:   "permissions",
				Rule:    "duplicate",
				Message: "duplicate permission code: " + code.String(),
			})
			continue
		}
		seen[code] = struct{}{}
	}

	return nil
}
