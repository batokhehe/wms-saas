package lookup

import (
	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"strings"
)

type item struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code"`
	Name string    `json:"name"`
}
type repository struct{ db *gorm.DB }

func (r repository) list(c *gin.Context, table, code, name, order string, company *uuid.UUID, search string) ([]item, error) {
	q := r.db.WithContext(appcontext.Context(c)).Table(table).Select("id, "+code+" AS code, "+name+" AS name").Where("status = ? AND deleted_at IS NULL", "ACTIVE").Order(order + " ASC")
	if company != nil {
		q = q.Where("company_id = ?", *company)
	}
	if search != "" {
		q = q.Where(code+" ILIKE ? OR "+name+" ILIKE ?", "%"+search+"%", "%"+search+"%")
	}
	var rows []item
	return rows, q.Find(&rows).Error
}

type handler struct {
	repo        repository
	verifier    middleware.TokenVerifier
	companies   middleware.CompanyResolver
	permissions middleware.PermissionResolver
}

func (h *handler) list(table, code, name, order string, global bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var companyID *uuid.UUID
		if !global {
			rc := appcontext.FromGin(c)
			companyID = rc.CompanyID
		}
		rows, err := h.repo.list(c, table, code, name, order, companyID, strings.TrimSpace(c.Query("search")))
		if err != nil {
			response.Error(c, err)
			return
		}
		response.OK(c, "Lookup retrieved successfully", rows)
	}
}
