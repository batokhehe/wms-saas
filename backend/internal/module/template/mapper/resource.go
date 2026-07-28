// Package mapper converts between entities and DTOs.
//
// LAYER RULE: conversion lives here and nowhere else. Handlers and services do
// not build DTOs inline. Centralising it means that when a field must be hidden
// from API output, there is exactly one place to change — and one place to
// review when asking "can this field leak?".
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/template/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/template/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ToResponse converts an entity into its API representation.
//
// ID, CreatedAt and UpdatedAt are read through the embedded BaseEntity, which
// is why every entity must embed it: the mapper can rely on those three fields
// existing without knowing anything else about the type.
func ToResponse(e *entity.Resource) dto.Response {
	if e == nil {
		return dto.Response{}
	}

	return dto.Response{
		ID:        e.ID,
		Name:      e.Name,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

// ToResponseList converts a slice of entities.
//
// It returns an empty slice rather than nil so the JSON encoder emits [] and
// not null. A client that has to handle both will eventually crash on one.
func ToResponseList(entities []entity.Resource) []dto.Response {
	responses := make([]dto.Response, 0, len(entities))

	for i := range entities {
		responses = append(responses, ToResponse(&entities[i]))
	}

	return responses
}

// ToResponsePage converts a page of entities into a page of DTOs, preserving
// the pagination metadata.
//
// This is the standard shape of a list use case's return value: the service
// hands the handler a fully-formed page and the handler writes it, with no
// re-assembly of items and totals in between.
func ToResponsePage(page pagination.Page[entity.Resource]) pagination.Page[dto.Response] {
	return pagination.MapPage(page, func(e entity.Resource) dto.Response {
		return ToResponse(&e)
	})
}

// FromCreateRequest builds a new entity from a create request.
//
// The tenant is passed separately and explicitly rather than being read from
// the request body: a client must never be able to choose which company a
// record belongs to.
//
// No ID is assigned here. Identifier generation belongs to the repository,
// which holds the injected port.IDGenerator — that is what keeps uuid.New() out
// of every module.
func FromCreateRequest(req dto.CreateRequest, companyID entity.TenantID) entity.Resource {
	return entity.Resource{
		CompanyID: companyID,
		Name:      req.Name,
	}
}

// ApplyUpdateRequest applies a partial update onto an existing entity, leaving
// omitted fields untouched.
func ApplyUpdateRequest(e *entity.Resource, req dto.UpdateRequest) {
	if req.Name != nil {
		e.Name = *req.Name
	}
}
