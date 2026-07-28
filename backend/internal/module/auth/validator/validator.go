// Package validator holds the auth module's custom validation rules.
//
// Struct-tag validation (required, email, max) is declared on the DTOs and
// handled by internal/shared/validator. This package holds the rules tags
// cannot express — principally password complexity.
package validator

import (
	"strings"
	"unicode"

	"github.com/batokhehe/wms-saas/backend/internal/module/auth/dto"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Password length bounds.
const (
	// MinPasswordLength is the floor. Eight characters with mixed classes is
	// the stated requirement; length matters far more than composition, which
	// is why the maximum is generous rather than restrictive.
	MinPasswordLength = 8

	// MaxPasswordLength is 72 because bcrypt ignores everything past 72 BYTES.
	//
	// This is not a stylistic cap. Without it, "correct horse battery staple
	// ..." truncated at 72 bytes would authenticate any longer password sharing
	// that prefix — a user with a 100-character passphrase would have the last
	// 28 characters silently ignored, and would never know their password is
	// weaker than they believe.
	MaxPasswordLength = 72
)

// ValidatePassword enforces the complexity policy.
//
// It reports EVERY violated rule rather than stopping at the first. A form that
// rejects a password once for "needs a digit" and then again for "needs a
// symbol" trains people to pick the weakest thing that finally passes.
func ValidatePassword(password string) error {
	var fields []apperror.FieldError

	add := func(rule, message string) {
		fields = append(fields, apperror.FieldError{
			Field:   "password",
			Rule:    rule,
			Message: message,
		})
	}

	// Length is measured in BYTES, not runes, because bcrypt's 72-byte limit is
	// a byte limit. A password of 30 emoji is 120 bytes and would be silently
	// truncated if this counted characters.
	switch length := len(password); {
	case length < MinPasswordLength:
		add("min", "password must be at least 8 characters")
	case length > MaxPasswordLength:
		add("max", "password must be at most 72 bytes")
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		// Anything that is not a letter, digit or space counts as special —
		// including Unicode punctuation and symbols. An allow-list of ASCII
		// symbols would reject a legitimate "café-naïve!" style passphrase.
		case unicode.IsPunct(r), unicode.IsSymbol(r):
			hasSpecial = true
		}
	}

	if !hasUpper {
		add("uppercase", "password must contain at least one uppercase letter")
	}
	if !hasLower {
		add("lowercase", "password must contain at least one lowercase letter")
	}
	if !hasDigit {
		add("digit", "password must contain at least one number")
	}
	if !hasSpecial {
		add("special", "password must contain at least one special character")
	}

	// A password made only of whitespace can satisfy a length check but is a
	// typo, not a credential.
	if strings.TrimSpace(password) == "" {
		add("not_blank", "password must not be blank")
	}

	if len(fields) > 0 {
		return apperror.NewValidation(fields...).WithOp("auth.validator.ValidatePassword")
	}

	return nil
}

// ValidateRegister applies the rules binding tags cannot express to a
// registration request.
func ValidateRegister(req dto.RegisterRequest) error {
	if err := ValidatePassword(req.Password); err != nil {
		return err
	}

	if strings.TrimSpace(req.FullName) == "" {
		return apperror.NewValidation(apperror.FieldError{
			Field:   "full_name",
			Rule:    "not_blank",
			Message: "full_name must not be blank",
		}).WithOp("auth.validator.ValidateRegister")
	}

	return nil
}
