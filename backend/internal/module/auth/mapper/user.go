// Package mapper converts between entities and DTOs.
//
// LAYER RULE: conversion lives here and nowhere else. For this module that rule
// carries the security weight of the whole package: this is the single place
// where a User becomes something a client can see, so it is the single place to
// review when asking "can the password hash leak?".
//
// The answer is structural rather than procedural — dto.UserResponse has no
// field capable of holding it — but centralising the conversion means a future
// field added to entity.User is opt-in to the API, not opt-out.
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/entity"
)

// ToUserResponse converts a user into its API representation.
func ToUserResponse(u *entity.User) dto.UserResponse {
	if u == nil {
		return dto.UserResponse{}
	}

	return dto.UserResponse{
		ID:       u.ID,
		Email:    u.Email,
		FullName: u.FullName,
		Status:   string(u.Status),
		// Exposed as a boolean as well as a timestamp: a client rendering a
		// "verify your email" banner wants the flag, not date arithmetic.
		EmailVerified:   u.IsEmailVerified(),
		LastLoginAt:     u.LastLoginAt,
		EmailVerifiedAt: u.EmailVerifiedAt,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

// FromRegisterRequest builds a new user from a registration request.
//
// The password hash is passed separately rather than being derived here: the
// mapper is a pure conversion and must not perform cryptography, both because
// hashing needs configuration it should not carry and because a mapper that can
// fail is no longer a mapper.
//
// The email is normalised on the way in, so the stored form is canonical.
func FromRegisterRequest(req dto.RegisterRequest, passwordHash string) entity.User {
	return entity.User{
		Email:        entity.NormalizeEmail(req.Email),
		PasswordHash: passwordHash,
		FullName:     req.FullName,
		// New accounts are active immediately. Email verification exists as a
		// field but gates nothing in this sprint; see entity.User.
		Status: entity.StatusActive,
	}
}
