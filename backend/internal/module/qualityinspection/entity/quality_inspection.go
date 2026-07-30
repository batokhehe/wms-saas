package entity

import (
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
	"github.com/google/uuid"
	"time"
)

type Status string

const (
	StatusDraft      Status = "DRAFT"
	StatusInProgress Status = "IN_PROGRESS"
	StatusPassed     Status = "PASSED"
	StatusFailed     Status = "FAILED"
)

type QualityInspection struct {
	id, companyID, goodsReceiptID, warehouseID, inspectorID uuid.UUID
	number                                                  string
	inspectionDate                                          time.Time
	status                                                  Status
	lines                                                   []QualityInspectionLine
	updatedAt                                               time.Time
}

type QualityInspectionLine struct {
	id, inspectionID, goodsReceiptLineID, productID uuid.UUID
	expectedQty, acceptedQty, rejectedQty           float64
	reason, remarks                                 string
}

func New(id uuid.UUID, number string, companyID, goodsReceiptID, warehouseID, inspectorID uuid.UUID, now time.Time) *QualityInspection {
	return &QualityInspection{
		id: id, number: number, companyID: companyID, goodsReceiptID: goodsReceiptID,
		warehouseID: warehouseID, inspectorID: inspectorID,
		status: StatusDraft, inspectionDate: now, updatedAt: now,
	}
}

func (qi *QualityInspection) Start() error {
	if qi.status != StatusDraft {
		return apperror.Conflict("must be draft")
	}
	qi.status = StatusInProgress
	return nil
}

func (qi *QualityInspection) Complete(accepted bool) error {
	if qi.status != StatusInProgress {
		return apperror.Conflict("must be in progress")
	}
	qi.status = map[bool]Status{true: StatusPassed, false: StatusFailed}[accepted]
	return nil
}

func (qi *QualityInspection) AddLine(line QualityInspectionLine) { qi.lines = append(qi.lines, line) }
func (qi *QualityInspection) ID() uuid.UUID                      { return qi.id }
func (qi *QualityInspection) Status() Status                     { return qi.status }
func (qi *QualityInspection) Lines() []QualityInspectionLine     { return qi.lines }
