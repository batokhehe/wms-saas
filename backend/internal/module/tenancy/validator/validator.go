// Package validator holds the tenancy module's custom validation rules.
//
// Struct-tag validation (required, oneof, max) is declared on the DTOs and
// handled by internal/shared/validator. This package holds the rules tags
// cannot express.
package validator

import (
	"regexp"
	"strings"

	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/entity"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// codePattern constrains a company code to letters and digits.
//
// It is deliberately narrow. The code appears in filenames, document headers,
// CSV exports and URLs, so permitting spaces, slashes or punctuation would mean
// escaping it correctly in every one of those places forever.
var codePattern = regexp.MustCompile(`^[A-Z0-9]{2,32}$`)

// reservedCodes cannot be claimed by a tenant.
//
// These collide with route segments and with internal identifiers. A company
// coded "API" or "ADMIN" would produce URLs and log lines that are ambiguous
// between a tenant and a system path.
var reservedCodes = map[string]struct{}{
	"API": {}, "ADMIN": {}, "SYSTEM": {}, "ROOT": {}, "PUBLIC": {},
	"INTERNAL": {}, "HEALTH": {}, "AUTH": {}, "STATIC": {}, "NULL": {},
}

// ValidateCreateCompany applies the rules binding tags cannot express.
//
// Every violation is reported rather than stopping at the first, so a form does
// not reject a submission once per problem.
func ValidateCreateCompany(req dto.CreateCompanyRequest) error {
	var fields []apperror.FieldError

	code := entity.NormalizeCode(req.Code)

	// The `alphanum` tag rejects punctuation but is case-insensitive and has no
	// length awareness of the normalised form, so the pattern is re-checked
	// against the canonical value that will actually be stored.
	if !codePattern.MatchString(code) {
		fields = append(fields, apperror.FieldError{
			Field:   "code",
			Rule:    "format",
			Message: "code must be 2-32 letters and digits",
		})
	}

	if _, reserved := reservedCodes[code]; reserved {
		fields = append(fields, apperror.FieldError{
			Field:   "code",
			Rule:    "reserved",
			Message: "this code is reserved and cannot be used",
		})
	}

	// `required` is satisfied by a string of spaces, which is a typo rather
	// than a name.
	if strings.TrimSpace(req.Name) == "" {
		fields = append(fields, apperror.FieldError{
			Field:   "name",
			Rule:    "not_blank",
			Message: "name must not be blank",
		})
	}

	if len(fields) > 0 {
		return apperror.NewValidation(fields...).
			WithOp("tenancy.validator.ValidateCreateCompany")
	}

	return nil
}

// ValidateUpdateCompany checks a partial update.
func ValidateUpdateCompany(req dto.UpdateCompanyRequest) error {
	var fields []apperror.FieldError

	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		fields = append(fields, apperror.FieldError{
			Field:   "name",
			Rule:    "not_blank",
			Message: "name must not be blank",
		})
	}

	if req.Status != nil && !entity.CompanyStatus(*req.Status).Valid() {
		fields = append(fields, apperror.FieldError{
			Field:   "status",
			Rule:    "oneof",
			Message: "status must be one of: ACTIVE, INACTIVE, SUSPENDED",
		})
	}

	if len(fields) > 0 {
		return apperror.NewValidation(fields...).
			WithOp("tenancy.validator.ValidateUpdateCompany")
	}

	return nil
}

// ValidateInvite checks an invitation request.
//
// OWNER is rejected. Ownership is a TRANSFER, not an invitation: a company
// always has exactly one founding owner, and creating a second by invitation
// would make "who can delete this company?" ambiguous with no way to resolve
// it. A dedicated transfer-ownership flow is the correct shape for that, and it
// belongs with RBAC in the next sprint.
//
// The tag on the DTO permits OWNER so that the rejection produces a clear,
// specific message here rather than a generic "must be one of" from the binder.
func ValidateInvite(req dto.InviteMemberRequest) error {
	role := entity.Role(strings.ToUpper(strings.TrimSpace(req.Role)))

	if role == entity.RoleOwner {
		return apperror.NewValidation(apperror.FieldError{
			Field:   "role",
			Rule:    "not_allowed",
			Message: "ownership cannot be granted by invitation; use ownership transfer",
		}).WithOp("tenancy.validator.ValidateInvite")
	}

	if !role.Valid() {
		return apperror.NewValidation(apperror.FieldError{
			Field:   "role",
			Rule:    "oneof",
			Message: "role must be one of: ADMIN, STAFF",
		}).WithOp("tenancy.validator.ValidateInvite")
	}

	return nil
}
