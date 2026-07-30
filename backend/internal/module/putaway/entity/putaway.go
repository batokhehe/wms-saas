package entity

import (
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
	"github.com/google/uuid"
	"time"
)

type Status string

const (
	StatusDraft     Status = "DRAFT"
	StatusConfirmed Status = "CONFIRMED"
	StatusCompleted Status = "COMPLETED"
	StatusCancelled Status = "CANCELLED"
)

type Putaway struct {
	id, companyID, warehouseID, goodsReceiptID, qualityInspectionID uuid.UUID
	number                                                          string
	status                                                          Status
	lines                                                           []PutawayLine
	version                                                         uint64
	updatedAt                                                       time.Time
}

type PutawayLine struct {
	id, putawayID, productID, fromLocationID, toLocationID uuid.UUID
	quantity                                               float64
}

func New(id uuid.UUID, number string, companyID, warehouseID uuid.UUID, now time.Time) *Putaway {
	return &Putaway{
		id: id, number: number, companyID: companyID, warehouseID: warehouseID,
		status: StatusDraft, updatedAt: now, version: 1,
	}
}

func (p *Putaway) Confirm() error {
	if p.status != StatusDraft {
		return apperror.Conflict("must be draft")
	}
	if len(p.lines) == 0 {
		return apperror.Validation("at least one line required")
	}
	p.status = StatusConfirmed
	return nil
}

func (p *Putaway) Complete(now time.Time) error {
	if p.status != StatusConfirmed {
		return apperror.Conflict("must be confirmed")
	}
	p.status = StatusCompleted
	p.updatedAt = now
	return nil
}

func (p *Putaway) Cancel() error {
	if p.status != StatusDraft && p.status != StatusConfirmed {
		return apperror.Conflict("cannot cancel")
	}
	p.status = StatusCancelled
	return nil
}

func (p *Putaway) AddLine(line PutawayLine) { p.lines = append(p.lines, line) }
func (p *Putaway) ID() uuid.UUID            { return p.id }
func (p *Putaway) Status() Status           { return p.status }
func (p *Putaway) Lines() []PutawayLine     { return p.lines }
