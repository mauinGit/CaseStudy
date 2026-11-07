package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
)

type AttendanceResponse struct {
	ID          uuid.UUID `json:"id"`
	EventID     uuid.UUID `json:"event_id"`
	UserID      uuid.UUID `json:"user_id"`
	CheckedInAt time.Time `json:"checked_in_at"`
}

type AttendanceRecordResponse struct {
	ID          uuid.UUID       `json:"id"`
	User        ParticipantUser `json:"user"`
	CheckedInAt time.Time       `json:"checked_in_at"`
}

type listAttendancesQuery struct {
	Page  int `form:"page,default=1" binding:"min=1,max=100000"`
	Limit int `form:"limit,default=20" binding:"min=1,max=100"`
}

// CheckIn records the caller's attendance.
//
//	@Summary		Check-in ke event
//	@Description	Hanya peserta terdaftar, hanya saat start_at <= sekarang <= end_at. Idempotent: check-in pertama 201, check-in berikutnya 200 dengan data yang sama (tidak membuat baris baru).
//	@Tags			attendances
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"ID event (UUID)"
//	@Success		201	{object}	dataResponse{data=AttendanceResponse}	"Check-in baru"
//	@Success		200	{object}	dataResponse{data=AttendanceResponse}	"Sudah check-in sebelumnya"
//	@Failure		401	{object}	apperror.Body
//	@Failure		403	{object}	apperror.Body	"NOT_REGISTERED"
//	@Failure		404	{object}	apperror.Body
//	@Failure		409	{object}	apperror.Body	"CHECK_IN_NOT_OPEN, EVENT_ENDED"
//	@Router			/api/v1/events/{id}/check-in [post]
func (h *EventHandler) CheckIn(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	a, created, err := h.events.CheckIn(c.Request.Context(), actor(c), id)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respond(c, status, AttendanceResponse{ID: a.ID, EventID: a.EventID, UserID: a.UserID, CheckedInAt: a.CheckedInAt})
}

// Attendances lists an event's check-ins.
//
//	@Summary		Daftar kehadiran event
//	@Description	Lead: semua. Division Head: semua di event divisinya, hanya anggota divisinya di event lain. Member: hanya data dirinya.
//	@Tags			attendances
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id		path		string	true	"ID event (UUID)"
//	@Param			page	query		int		false	"Halaman (mulai 1)"				default(1)
//	@Param			limit	query		int		false	"Jumlah per halaman (maks 100)"	default(20)
//	@Success		200		{object}	pageResponse{data=[]AttendanceRecordResponse}
//	@Failure		400		{object}	apperror.Body
//	@Failure		401		{object}	apperror.Body
//	@Failure		404		{object}	apperror.Body
//	@Router			/api/v1/events/{id}/attendances [get]
func (h *EventHandler) Attendances(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	var q listAttendancesQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.events.ListAttendances(c.Request.Context(), actor(c), domain.AttendanceFilter{
		EventID: id, Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	items := make([]AttendanceRecordResponse, 0, len(page.Items))
	for _, a := range page.Items {
		items = append(items, AttendanceRecordResponse{
			ID: a.ID,
			User: ParticipantUser{
				ID: a.UserID, Name: a.UserName, Email: a.UserEmail,
				Division: divisionRef(a.DivisionID, a.DivisionName),
			},
			CheckedInAt: a.CheckedInAt,
		})
	}
	c.JSON(http.StatusOK, pageResponse{Data: items, Meta: toPageMeta(page)})
}
