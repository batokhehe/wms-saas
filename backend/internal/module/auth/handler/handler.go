// Package handler is the auth module's HTTP layer.
//
// LAYER RULE: bind input, call the service, shape the response, return. No
// business logic, no persistence, no c.JSON.
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/module/auth/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// Handler exposes the auth module over HTTP.
type Handler struct {
	service *service.Service
}

// New wires the handler to its service.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// Register handles POST /auth/register.
func (h *Handler) Register(c *gin.Context) {
	var req dto.RegisterRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Register(appcontext.Context(c), req, sessionFrom(c))
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, "Account created successfully", result)
}

// Login handles POST /auth/login.
func (h *Handler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Login(appcontext.Context(c), req, sessionFrom(c))
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Signed in successfully", result)
}

// Refresh handles POST /auth/refresh.
func (h *Handler) Refresh(c *gin.Context) {
	var req dto.RefreshRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Refresh(appcontext.Context(c), req, sessionFrom(c))
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Token refreshed successfully", result)
}

// Logout handles POST /auth/logout.
func (h *Handler) Logout(c *gin.Context) {
	var req dto.LogoutRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	if err := h.service.Logout(appcontext.Context(c), req); err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Signed out successfully", nil)
}

// Me handles GET /auth/me. It is mounted behind the JWT middleware, so the
// user id is already on the request context.
func (h *Handler) Me(c *gin.Context) {
	result, err := h.service.Me(appcontext.Context(c))
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Profile retrieved successfully", result)
}

// sessionFrom assembles request provenance for the session record.
//
// This is the one place HTTP details are read, so the service never sees a
// *gin.Context. ClientIP is taken from Gin, which applies the trusted-proxy
// configuration — a raw X-Forwarded-For header would be client-controlled and
// would let an attacker forge the audit trail of their own session.
func sessionFrom(c *gin.Context) service.SessionContext {
	return service.SessionContext{
		IPAddress: c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}
}
