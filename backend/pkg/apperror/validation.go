package apperror

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

// FieldError is a single field-level validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// ValidationDetails is the Details payload of a VALIDATION_ERROR.
//
// It is a named type rather than a bare map so the shape is part of the API
// contract and the client can deserialise it into a typed model.
type ValidationDetails struct {
	Fields []FieldError `json:"fields"`
}

// NewValidation builds a validation error from explicit field failures.
func NewValidation(fields ...FieldError) *Error {
	return Validation("The request contains invalid fields").
		WithDetails(ValidationDetails{Fields: fields})
}

// FromValidator converts a go-playground/validator error into a validation
// error with per-field details.
//
// Centralising this conversion is what stops every handler from inventing its
// own error shape: bind, call this, return. The Flutter client can then map
// failures onto form fields uniformly across every endpoint.
func FromValidator(err error) *Error {
	if err == nil {
		return nil
	}

	// An InvalidValidationError means the validator was misused (a nil or
	// non-struct target). That is a programming bug, not a user input problem,
	// so it must not be reported as a 422.
	var invalid *validator.InvalidValidationError
	if errors.As(err, &invalid) {
		return Internal("Request validation was misconfigured").WithCause(err)
	}

	var validationErrs validator.ValidationErrors
	if !errors.As(err, &validationErrs) {
		// Not a validator error: most often a JSON type mismatch from binding.
		return BadRequest("The request body is malformed").WithCause(err)
	}

	fields := make([]FieldError, 0, len(validationErrs))
	for _, fieldErr := range validationErrs {
		fields = append(fields, FieldError{
			Field:   fieldName(fieldErr),
			Rule:    fieldErr.Tag(),
			Message: describeRule(fieldErr),
		})
	}

	return NewValidation(fields...)
}

// fieldName prefers the JSON name so the client sees the same identifier it
// sent, not the Go struct field name.
func fieldName(fe validator.FieldError) string {
	if name := fe.Field(); name != "" {
		return name
	}
	return fe.StructField()
}

// describeRule renders a rule violation in plain language. Raw tag names like
// "gte" mean nothing to an end user staring at a form.
func describeRule(fe validator.FieldError) string {
	field := fieldName(fe)

	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "email":
		return fmt.Sprintf("%s must be a valid email address", field)
	case "uuid", "uuid4":
		return fmt.Sprintf("%s must be a valid UUID", field)
	case "min":
		return fmt.Sprintf("%s must be at least %s", field, fe.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", field, fe.Param())
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", field, fe.Param())
	case "gte":
		return fmt.Sprintf("%s must be greater than or equal to %s", field, fe.Param())
	case "lt":
		return fmt.Sprintf("%s must be less than %s", field, fe.Param())
	case "lte":
		return fmt.Sprintf("%s must be less than or equal to %s", field, fe.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, strings.ReplaceAll(fe.Param(), " ", ", "))
	case "len":
		return fmt.Sprintf("%s must be exactly %s characters", field, fe.Param())
	default:
		return fmt.Sprintf("%s failed the %q rule", field, fe.Tag())
	}
}
