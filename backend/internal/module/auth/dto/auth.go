// Package dto holds the auth module's transport contracts.
//
// LAYER RULE: DTOs are never entities and entities are never DTOs. Here that
// rule is also a security control — entity.User has a PasswordHash field and
// UserResponse does not, so the hash cannot reach a client even by accident.
package dto

import (
	"time"

	"github.com/google/uuid"
)

// ---------- Requests ----------

// RegisterRequest creates an account.
//
// The password rules are enforced in validator/, not as binding tags. `min=8`
// and `max=72` are expressible as tags, but "must contain an uppercase letter,
// a lowercase letter, a digit and a special character" is not — and splitting
// the rules across two mechanisms means two places to look and two places to
// change. All of them live together in the validator.
type RegisterRequest struct {
	Email    string `json:"email"    binding:"required,email,max=255"`
	Password string `json:"password" binding:"required"`
	FullName string `json:"full_name" binding:"required,min=1,max=255"`
}

// LoginRequest authenticates an account.
//
// The password field carries no complexity binding. Applying the registration
// rules here would tell an attacker which candidate passwords are even worth
// submitting, turning the login endpoint into a free filter for a credential
// list.
type LoginRequest struct {
	Email    string `json:"email"    binding:"required,email,max=255"`
	Password string `json:"password" binding:"required,max=72"`

	// Device is a client-supplied label ("Warehouse Scanner 12", "iPhone 15")
	// shown in the session list. Untrusted and display-only.
	Device string `json:"device" binding:"omitempty,max=255"`
}

// RefreshRequest exchanges a refresh token for a new token pair.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required,min=32,max=512"`
}

// LogoutRequest revokes a refresh token.
//
// The refresh token identifies the session to end. The access token cannot: it
// is stateless and carries no session identity, so logging out by access token
// would either end every session or none.
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required,min=32,max=512"`

	// AllSessions ends every session for this user, for "sign out everywhere"
	// after a suspected compromise.
	AllSessions bool `json:"all_sessions"`
}

// ---------- Responses ----------

// TokenPair is what a successful register, login or refresh returns.
type TokenPair struct {
	AccessToken string `json:"access_token"`

	// RefreshToken is the only moment the raw value exists outside the client.
	// The server stores its SHA-256 digest and cannot reproduce this string.
	RefreshToken string `json:"refresh_token"`

	TokenType string `json:"token_type"`

	// ExpiresIn is the access token lifetime in seconds, so a client can
	// schedule a refresh instead of waiting for a 401. Seconds rather than an
	// absolute timestamp because it is immune to client clock skew.
	ExpiresIn int64 `json:"expires_in"`

	// RefreshExpiresAt is absolute: it is far enough out that skew is
	// irrelevant, and a client needs it to know when re-authentication is due.
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// AuthResponse is returned by register and login: the tokens plus the identity
// they belong to, so a client does not need a second call to render a profile.
type AuthResponse struct {
	User   UserResponse `json:"user"`
	Tokens TokenPair    `json:"tokens"`
}

// UserResponse is the public representation of an account.
//
// There is no PasswordHash field, by construction. The mapper cannot leak what
// the type cannot hold.
type UserResponse struct {
	ID              uuid.UUID  `json:"id"`
	Email           string     `json:"email"`
	FullName        string     `json:"full_name"`
	Status          string     `json:"status"`
	EmailVerified   bool       `json:"email_verified"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
