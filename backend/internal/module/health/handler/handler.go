// Package handler is the health module's HTTP layer.
//
// Handlers do four things and nothing else: read input, call the service,
// convert the result, and hand it to the response package. No business logic,
// no direct c.JSON calls.
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/module/health/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/health/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Handler exposes the health service over HTTP.
type Handler struct {
	service *service.Service
}

// New wires the handler to its service.
func New(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// Live answers the liveness probe.
//
//	GET /health/live
func (h *Handler) Live(c *gin.Context) {
	response.OK(c, "Service is alive", h.service.Live())
}

// Ready answers the readiness probe.
//
// A degraded report is returned through response.Error rather than as a success
// envelope with a "down" status, so the failure is expressed the same way as
// every other error in the API: success=false with a machine-readable code.
//
//	GET /health/ready
func (h *Handler) Ready(c *gin.Context) {
	// appcontext.Context carries the cancellation of the inbound request, so a
	// client that disconnects also cancels the dependency probes.
	report := h.service.Ready(appcontext.Context(c))

	if report.Status == dto.StatusDown {
		response.Error(c, apperror.Unavailable("One or more dependencies are unavailable").
			WithDetails(report).
			WithOp("health.Ready"))
		return
	}

	response.OK(c, "Service is ready", report)
}
