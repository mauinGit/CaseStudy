package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
	"github.com/mauinGit/gdgoc-core-team-api/internal/service"
)

type AuditHandler struct {
	audit *service.AuditService
}

func NewAuditHandler(audit *service.AuditService) *AuditHandler {
	return &AuditHandler{audit: audit}
}

type listAuditQuery struct {
	Page     int    `form:"page,default=1" binding:"min=1,max=100000"`
	Limit    int    `form:"limit,default=20" binding:"min=1,max=100"`
	Action   string `form:"action" binding:"omitempty,oneof=member.role_changed member.deleted event.quota_changed"`
	Entity   string `form:"entity" binding:"omitempty,oneof=user event"`
	EntityID string `form:"entity_id" binding:"omitempty,uuid"`
	ActorID  string `form:"actor_id" binding:"omitempty,uuid"`
}

type AuditActor struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type AuditLogResponse struct {
	ID        int64           `json:"id"`
	Actor     *AuditActor     `json:"actor"`
	Action    string          `json:"action" enums:"member.role_changed,member.deleted,event.quota_changed"`
	Entity    string          `json:"entity" enums:"user,event"`
	EntityID  string          `json:"entity_id"`
	OldValue  json.RawMessage `json:"old_value" swaggertype:"object"`
	NewValue  json.RawMessage `json:"new_value" swaggertype:"object"`
	IP        *string         `json:"ip"`
	CreatedAt time.Time       `json:"created_at"`
}

func rawOrNull(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return b
}

// List returns audit logs.
//
//	@Summary		Audit log
//	@Description	Hanya Lead. Urut dari yang terbaru. Mencatat perubahan role, soft delete anggota, dan perubahan kuota event.
//	@Tags			audit
//	@Security		BearerAuth
//	@Produce		json
//	@Param			page		query		int		false	"Halaman (mulai 1)"				default(1)
//	@Param			limit		query		int		false	"Jumlah per halaman (maks 100)"	default(20)
//	@Param			action		query		string	false	"Filter aksi"					Enums(member.role_changed, member.deleted, event.quota_changed)
//	@Param			entity		query		string	false	"Filter entitas"				Enums(user, event)
//	@Param			entity_id	query		string	false	"Filter ID entitas (UUID)"
//	@Param			actor_id	query		string	false	"Filter pelaku (UUID)"
//	@Success		200			{object}	pageResponse{data=[]AuditLogResponse}
//	@Failure		400			{object}	apperror.Body
//	@Failure		401			{object}	apperror.Body
//	@Failure		403			{object}	apperror.Body
//	@Router			/api/v1/audit-logs [get]
func (h *AuditHandler) List(c *gin.Context) {
	var q listAuditQuery
	if !bindQuery(c, &q) {
		return
	}
	f := domain.AuditFilter{Action: q.Action, Entity: q.Entity, EntityID: q.EntityID, Page: q.Page, Limit: q.Limit}
	if q.ActorID != "" {
		id := uuid.MustParse(q.ActorID)
		f.ActorID = &id
	}
	page, err := h.audit.List(c.Request.Context(), actor(c), f)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	items := make([]AuditLogResponse, 0, len(page.Items))
	for _, l := range page.Items {
		item := AuditLogResponse{
			ID: l.ID, Action: l.Action, Entity: l.Entity, EntityID: l.EntityID,
			OldValue: rawOrNull(l.OldValue), NewValue: rawOrNull(l.NewValue),
			IP: l.IP, CreatedAt: l.CreatedAt,
		}
		if l.ActorID != nil {
			item.Actor = &AuditActor{ID: *l.ActorID}
			if l.ActorName != nil {
				item.Actor.Name = *l.ActorName
			}
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, pageResponse{Data: items, Meta: toPageMeta(page)})
}
