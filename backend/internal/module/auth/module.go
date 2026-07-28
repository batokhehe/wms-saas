// Package auth implements identity and authentication.
//
// It is the identity foundation of the platform, not merely a login endpoint,
// and it is deliberately independent of Company:
//
//   - entity.User has no CompanyID and never will.
//   - Access tokens carry no company claim.
//   - No repository method takes a companyID.
//
// A person can belong to several companies — a 3PL operator works for multiple
// clients — so binding identity to a tenant would mean duplicate accounts,
// duplicate credentials and no single account to lock when that person leaves.
// Sprint 2 adds a memberships module joining users to companies; nothing in
// this package changes when it does.
package auth

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/config"
	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/handler"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/route"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/module"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Module is the auth vertical slice.
type Module struct {
	handler  *handler.Handler
	verifier *tokenVerifier
}

// Compile-time assertions. If a contract method drifts, the build fails here
// rather than the module silently vanishing from the router.
var (
	_ module.Module      = (*Module)(nil)
	_ module.V1Registrar = (*Module)(nil)
)

// New constructs the module and its internal dependency graph.
//
// Manual DI, readable top to bottom: repositories, then the security
// primitives, then the service, then the handler. Nothing is resolved from a
// container and no framework is needed to follow it.
func New(deps module.Dependencies, cfg config.AuthConfig) *Module {
	users := repository.NewUserRepository(deps.DB, deps.IDs)
	tokens := repository.NewRefreshTokenRepository(deps.DB, deps.IDs)

	hasher := service.NewBcryptHasher(cfg.Password.BcryptCost)
	tokenSvc := service.NewTokenService(cfg.JWT, deps.Clock, deps.IDs)

	// Audit events are published to the structured log. No handlers exist in
	// this sprint; see service/publisher.go for why the queue is deliberately
	// not used yet.
	events := service.NewLogEventPublisher(deps.Logger)

	svc := service.New(
		users, tokens, hasher, tokenSvc,
		deps.Clock, deps.Tx, events,
		cfg.JWT.MaxSessionsPerUser,
	)

	return &Module{
		handler:  handler.New(svc),
		verifier: &tokenVerifier{tokens: tokenSvc},
	}
}

// Name identifies the module.
func (m *Module) Name() string { return "auth" }

// Verifier exposes the access-token verifier so bootstrap can hand it to other
// modules' protected routes.
//
// It returns middleware.TokenVerifier — an interface owned by the consumer, not
// by this module. Other modules therefore depend on the middleware package, not
// on auth, which is what keeps ModuleConvention's no-cross-module-imports rule
// intact for the one dependency every module eventually needs.
func (m *Module) Verifier() middleware.TokenVerifier { return m.verifier }

// RegisterV1 mounts the module under /api/v1.
func (m *Module) RegisterV1(rg *gin.RouterGroup) {
	route.RegisterV1(rg, m.handler, m.verifier)
}

// tokenVerifier adapts the module's TokenService to middleware.TokenVerifier.
//
// The adapter exists because the two types speak different vocabularies: the
// service returns JWT claims, while the middleware wants a Principal and knows
// nothing about JWT. Translating here keeps the middleware free of any token
// format, so swapping JWT for opaque tokens later would not touch it.
type tokenVerifier struct {
	tokens *service.TokenService
}

var _ middleware.TokenVerifier = (*tokenVerifier)(nil)

func (v *tokenVerifier) VerifyAccessToken(raw string) (middleware.Principal, error) {
	claims, err := v.tokens.VerifyAccessToken(raw)
	if err != nil {
		return middleware.Principal{}, err
	}

	userID, err := claims.UserID()
	if err != nil {
		return middleware.Principal{}, apperror.Unauthorized(
			"The access token is invalid or has expired").
			WithOp("auth.tokenVerifier.VerifyAccessToken").
			WithCause(err)
	}

	// Role stays empty: RBAC arrives with membership in Sprint 2.
	return middleware.Principal{UserID: userID}, nil
}
