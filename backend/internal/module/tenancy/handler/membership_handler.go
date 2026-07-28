package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/service"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
)

// MembershipHandler exposes the member-management use cases over HTTP.
type MembershipHandler struct {
	service *service.MembershipService
}

// NewMembershipHandler wires the handler to its service.
func NewMembershipHandler(svc *service.MembershipService) *MembershipHandler {
	return &MembershipHandler{service: svc}
}

// List handles GET /memberships — the members of the active company.
func (h *MembershipHandler) List(c *gin.Context) {
	var query dto.ListMembershipsQuery
	if err := validator.BindQuery(c, &query); err != nil {
		response.Error(c, err)
		return
	}

	page, err := h.service.List(appcontext.Context(c), query)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Page(c, "Members retrieved successfully", page)
}

// Mine handles GET /memberships/mine — the companies the caller can act in.
//
// Deliberately NOT behind RequireCompany: this is what a user with no active
// company calls to discover which ones they can switch to, so requiring a
// company context would make it unreachable for exactly the callers who need
// it.
func (h *MembershipHandler) Mine(c *gin.Context) {
	result, err := h.service.Mine(appcontext.Context(c))
	if err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Memberships retrieved successfully", result)
}

// Invite handles POST /memberships/invite.
func (h *MembershipHandler) Invite(c *gin.Context) {
	var req dto.InviteMemberRequest
	if err := validator.BindJSON(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	result, err := h.service.Invite(appcontext.Context(c), req)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, "Invitation created successfully", result)
}

// Remove handles DELETE /memberships/:id.
func (h *MembershipHandler) Remove(c *gin.Context) {
	var param dto.IDParam
	if err := validator.BindURI(c, &param); err != nil {
		response.Error(c, err)
		return
	}

	id, err := param.UUID()
	if err != nil {
		response.Error(c, err)
		return
	}

	if err := h.service.Remove(appcontext.Context(c), id); err != nil {
		response.Error(c, err)
		return
	}

	response.OK(c, "Member removed successfully", nil)
}
