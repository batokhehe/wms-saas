package validator

import (
	"errors"
	"strings"
	"testing"

	"github.com/batokhehe/wms-saas/backend/internal/module/auth/dto"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

func TestValidatePasswordAcceptsCompliant(t *testing.T) {
	for _, password := range []string{
		"Str0ng!Passw0rd",
		"Aa1!aaaa",                     // exactly the 8-character minimum
		"Café-Naïve1",                  // non-ASCII letters, Unicode punctuation
		"P@ssw0rd with spaces is fine", // spaces do not disqualify
		strings.Repeat("Aa1!", 18),     // exactly 72 bytes
	} {
		if err := ValidatePassword(password); err != nil {
			t.Errorf("ValidatePassword(%q) = %v, want nil", password, err)
		}
	}
}

func TestValidatePasswordRejects(t *testing.T) {
	tests := map[string]struct {
		password string
		wantRule string
	}{
		"too short":     {"Aa1!aaa", "min"},
		"no uppercase":  {"str0ng!passw0rd", "uppercase"},
		"no lowercase":  {"STR0NG!PASSW0RD", "lowercase"},
		"no digit":      {"Strong!Password", "digit"},
		"no special":    {"Str0ngPassw0rd1", "special"},
		"over 72 bytes": {strings.Repeat("Aa1!", 19), "max"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if err == nil {
				t.Fatalf("ValidatePassword(%q) = nil, want an error", tt.password)
			}

			var appErr *apperror.Error
			if !errors.As(err, &appErr) {
				t.Fatalf("error is %T, want *apperror.Error", err)
			}
			if appErr.Code != apperror.CodeValidation {
				t.Errorf("code = %s, want VALIDATION_ERROR", appErr.Code)
			}

			details, ok := appErr.Details.(apperror.ValidationDetails)
			if !ok {
				t.Fatalf("details = %#v, want ValidationDetails", appErr.Details)
			}

			var found bool
			for _, field := range details.Fields {
				if field.Rule == tt.wantRule {
					found = true
				}
				if field.Field != "password" {
					t.Errorf("field = %q, want password", field.Field)
				}
			}
			if !found {
				t.Errorf("no violation reported for rule %q; got %+v", tt.wantRule, details.Fields)
			}
		})
	}
}

// TestValidatePasswordReportsEveryViolation matters for usability: a form that
// rejects a password once for "needs a digit" and again for "needs a symbol"
// trains people to pick the weakest thing that finally passes.
func TestValidatePasswordReportsEveryViolation(t *testing.T) {
	err := ValidatePassword("abc") // short, no upper, no digit, no special

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is %T, want *apperror.Error", err)
	}

	details := appErr.Details.(apperror.ValidationDetails)
	if len(details.Fields) < 4 {
		t.Errorf("reported %d violations, want at least 4: %+v",
			len(details.Fields), details.Fields)
	}
}

// TestLengthIsMeasuredInBytes pins the bcrypt boundary. A password of 20 emoji
// is 20 runes but 80 bytes, and bcrypt would silently ignore everything past
// byte 72 — leaving the user with a weaker credential than they believe.
func TestLengthIsMeasuredInBytes(t *testing.T) {
	emoji := strings.Repeat("😀", 20) + "Aa1!"

	if len([]rune(emoji)) > MaxPasswordLength {
		t.Fatalf("test setup: %d runes already exceeds the limit", len([]rune(emoji)))
	}
	if len(emoji) <= MaxPasswordLength {
		t.Fatalf("test setup: %d bytes does not exceed the limit", len(emoji))
	}

	if err := ValidatePassword(emoji); err == nil {
		t.Error("ValidatePassword() accepted a password over 72 BYTES; bcrypt would truncate it")
	}
}

func TestValidateRegisterRejectsBlankName(t *testing.T) {
	err := ValidateRegister(dto.RegisterRequest{
		Email:    "ops@example.com",
		Password: "Str0ng!Passw0rd",
		FullName: "   ",
	})

	if err == nil {
		t.Fatal("ValidateRegister() = nil for a whitespace-only name")
	}
	if code := apperror.From(err).Code; code != apperror.CodeValidation {
		t.Errorf("code = %s, want VALIDATION_ERROR", code)
	}
}
