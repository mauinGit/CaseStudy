package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
	"github.com/mauinGit/gdgoc-core-team-api/internal/service"
)

type EventHandler struct {
	events *service.EventService
	now    func() time.Time
}

func NewEventHandler(events *service.EventService) *EventHandler {
	return &EventHandler{events: events, now: time.Now}
}

// ---- request DTOs ----

type listEventsQuery struct {
	Page       int    `form:"page,default=1" binding:"min=1,max=100000"`
	Limit      int    `form:"limit,default=20" binding:"min=1,max=100"`
	DivisionID string `form:"division_id" binding:"omitempty,uuid"`
	Status     string `form:"status" binding:"omitempty,oneof=upcoming ongoing finished"`
	Search     string `form:"search" binding:"max=100"`
	Sort       string `form:"sort,default=start_at" binding:"oneof=start_at created_at title"`
	Order      string `form:"order,default=asc" binding:"oneof=asc desc"`
}

type createEventRequest struct {
	Title       string    `json:"title" binding:"required,notblank,min=3,max=150"`
	Description string    `json:"description" binding:"max=5000"`
	DivisionID  *string   `json:"division_id" binding:"omitempty,uuid"`
	Location    string    `json:"location" binding:"required,notblank,max=200"`
	StartAt     time.Time `json:"start_at" binding:"required"`
	EndAt       time.Time `json:"end_at" binding:"required,gtfield=StartAt"`
	Quota       int       `json:"quota" binding:"required,min=1,max=100000"`
}

// updateEventRequest declares only editable fields: division, registered_count
// and creator cannot be changed through it.
type updateEventRequest struct {
	Title       *string    `json:"title" binding:"omitempty,notblank,min=3,max=150"`
	Description *string    `json:"description" binding:"omitempty,max=5000"`
	Location    *string    `json:"location" binding:"omitempty,notblank,max=200"`
	StartAt     *time.Time `json:"start_at"`
	EndAt       *time.Time `json:"end_at"`
	Quota       *int       `json:"quota" binding:"omitempty,min=1,max=100000"`
}

type listParticipantsQuery struct {
	Page   int    `form:"page,default=1" binding:"min=1,max=100000"`
	Limit  int    `form:"limit,default=20" binding:"min=1,max=100"`
	Status string `form:"status,default=registered" binding:"oneof=registered cancelled"`
}

// ---- responses ----

type EventResponse struct {
	ID              uuid.UUID          `json:"id"`
	Title           string             `json:"title"`
	Description     string             `json:"description"`
	Division        *DivisionRef       `json:"division"`
	Location        string             `json:"location"`
	StartAt         time.Time          `json:"start_at"`
	EndAt           time.Time          `json:"end_at"`
	Status          domain.EventStatus `json:"status" enums:"upcoming,ongoing,finished"`
	Quota           int                `json:"quota"`
	RegisteredCount int                `json:"registered_count"`
	RemainingQuota  int                `json:"remaining_quota"`
	CreatedBy       uuid.UUID          `json:"created_by"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

func (h *EventHandler) toEventResponse(e *domain.Event) EventResponse {
	return EventResponse{
		ID:              e.ID,
		Title:           e.Title,
		Description:     e.Description,
		Division:        divisionRef(e.DivisionID, e.DivisionName),
		Location:        e.Location,
		StartAt:         e.StartAt,
		EndAt:           e.EndAt,
		Status:          e.StatusAt(h.now()),
		Quota:           e.Quota,
		RegisteredCount: e.RegisteredCount,
		RemainingQuota:  e.Remaining(),
		CreatedBy:       e.CreatedBy,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}
}

type RegistrationResponse struct {
	ID        uuid.UUID                 `json:"id"`
	EventID   uuid.UUID                 `json:"event_id"`
	UserID    uuid.UUID                 `json:"user_id"`
	Status    domain.RegistrationStatus `json:"status" enums:"registered,cancelled"`
	CreatedAt time.Time                 `json:"created_at"`
	UpdatedAt time.Time                 `json:"updated_at"`
}

func toRegistrationResponse(r *domain.Registration) RegistrationResponse {
	return RegistrationResponse{
		ID: r.ID, EventID: r.EventID, UserID: r.UserID, Status: r.Status,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

type ParticipantUser struct {
	ID       uuid.UUID    `json:"id"`
	Name     string       `json:"name"`
	Email    string       `json:"email"`
	Division *DivisionRef `json:"division"`
}

type ParticipantResponse struct {
	RegistrationID uuid.UUID                 `json:"registration_id"`
	User           ParticipantUser           `json:"user"`
	Status         domain.RegistrationStatus `json:"status" enums:"registered,cancelled"`
	RegisteredAt   time.Time                 `json:"registered_at"`
	UpdatedAt      time.Time                 `json:"updated_at"`
}

func divisionRef(id *uuid.UUID, name *string) *DivisionRef {
	if id == nil {
		return nil
	}
	ref := &DivisionRef{ID: *id}
	if name != nil {
		ref.Name = *name
	}
	return ref
}

// ---- handlers ----

// List lists events.
//
//	@Summary	Daftar event
//	@Tags		events
//	@Security	BearerAuth
//	@Produce	json
//	@Param		page		query		int		false	"Halaman (mulai 1)"				default(1)
//	@Param		limit		query		int		false	"Jumlah per halaman (maks 100)"	default(20)
//	@Param		division_id	query		string	false	"Filter divisi (UUID)"
//	@Param		status		query		string	false	"Filter status waktu"			Enums(upcoming, ongoing, finished)
//	@Param		search		query		string	false	"Cari judul (case-insensitive)"
//	@Param		sort		query		string	false	"Kolom urutan"					Enums(start_at, created_at, title)	default(start_at)
//	@Param		order		query		string	false	"Arah urutan"					Enums(asc, desc)					default(asc)
//	@Success	200			{object}	pageResponse{data=[]EventResponse}
//	@Failure	400			{object}	apperror.Body
//	@Failure	401			{object}	apperror.Body
//	@Router		/api/v1/events [get]
func (h *EventHandler) List(c *gin.Context) {
	var q listEventsQuery
	if !bindQuery(c, &q) {
		return
	}
	f := domain.EventFilter{
		Search: strings.TrimSpace(q.Search),
		Sort:   domain.EventSort(q.Sort),
		Desc:   q.Order == "desc",
		Page:   q.Page,
		Limit:  q.Limit,
	}
	if q.DivisionID != "" {
		id := uuid.MustParse(q.DivisionID)
		f.DivisionID = &id
	}
	if q.Status != "" {
		st := domain.EventStatus(q.Status)
		f.Status = &st
	}
	page, err := h.events.ListEvents(c.Request.Context(), f)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	items := make([]EventResponse, 0, len(page.Items))
	for _, e := range page.Items {
		items = append(items, h.toEventResponse(e))
	}
	c.JSON(http.StatusOK, pageResponse{Data: items, Meta: toPageMeta(page)})
}

// Get returns one event.
//
//	@Summary	Detail event
//	@Tags		events
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		string	true	"ID event (UUID)"
//	@Success	200	{object}	dataResponse{data=EventResponse}
//	@Failure	400	{object}	apperror.Body
//	@Failure	401	{object}	apperror.Body
//	@Failure	404	{object}	apperror.Body
//	@Router		/api/v1/events/{id} [get]
func (h *EventHandler) Get(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	e, err := h.events.GetEvent(c.Request.Context(), id)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	respond(c, http.StatusOK, h.toEventResponse(e))
}

// Create creates an event.
//
//	@Summary		Buat event
//	@Description	Lead: event divisi mana pun atau event umum (division_id kosong). Division Head: hanya event divisinya (division_id default ke divisinya). Member: ditolak.
//	@Tags			events
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		createEventRequest	true	"Data event (waktu dalam RFC3339)"
//	@Success		201		{object}	dataResponse{data=EventResponse}
//	@Failure		400		{object}	apperror.Body
//	@Failure		401		{object}	apperror.Body
//	@Failure		403		{object}	apperror.Body
//	@Failure		422		{object}	apperror.Body
//	@Router			/api/v1/events [post]
func (h *EventHandler) Create(c *gin.Context) {
	var req createEventRequest
	if !bindJSON(c, &req) {
		return
	}
	e, err := h.events.CreateEvent(c.Request.Context(), actor(c), service.CreateEventInput{
		Title:       req.Title,
		Description: req.Description,
		DivisionID:  parseOptionalUUID(req.DivisionID),
		Location:    req.Location,
		StartAt:     req.StartAt,
		EndAt:       req.EndAt,
		Quota:       req.Quota,
	})
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	c.Header("Location", "/api/v1/events/"+e.ID.String())
	respond(c, http.StatusCreated, h.toEventResponse(e))
}

// Update edits an event.
//
//	@Summary		Ubah event
//	@Description	Lead: semua event. Division Head: event divisinya. Perubahan kuota tidak boleh di bawah jumlah peserta terdaftar dan tercatat di audit log.
//	@Tags			events
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"ID event (UUID)"
//	@Param			body	body		updateEventRequest	true	"Field yang diubah"
//	@Success		200		{object}	dataResponse{data=EventResponse}
//	@Failure		400		{object}	apperror.Body
//	@Failure		401		{object}	apperror.Body
//	@Failure		403		{object}	apperror.Body
//	@Failure		404		{object}	apperror.Body
//	@Failure		409		{object}	apperror.Body
//	@Router			/api/v1/events/{id} [patch]
func (h *EventHandler) Update(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	var req updateEventRequest
	if !bindJSON(c, &req) {
		return
	}
	e, err := h.events.UpdateEvent(c.Request.Context(), actor(c), id, service.EventPatch{
		Title:       req.Title,
		Description: req.Description,
		Location:    req.Location,
		StartAt:     req.StartAt,
		EndAt:       req.EndAt,
		Quota:       req.Quota,
	})
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	respond(c, http.StatusOK, h.toEventResponse(e))
}

// Delete soft-deletes an event.
//
//	@Summary	Hapus event (soft delete)
//	@Tags		events
//	@Security	BearerAuth
//	@Param		id	path	string	true	"ID event (UUID)"
//	@Success	204
//	@Failure	400	{object}	apperror.Body
//	@Failure	401	{object}	apperror.Body
//	@Failure	403	{object}	apperror.Body
//	@Failure	404	{object}	apperror.Body
//	@Router		/api/v1/events/{id} [delete]
func (h *EventHandler) Delete(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	if err := h.events.DeleteEvent(c.Request.Context(), actor(c), id); err != nil {
		apperror.Respond(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Register registers the caller for an event.
//
//	@Summary		Daftar ke event
//	@Description	Semua role. Kuota dijaga atomik di database: tidak pernah terlampaui walau banyak request bersamaan. Mendaftar ulang setelah membatalkan diperbolehkan.
//	@Tags			registrations
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"ID event (UUID)"
//	@Success		201	{object}	dataResponse{data=RegistrationResponse}
//	@Failure		401	{object}	apperror.Body
//	@Failure		404	{object}	apperror.Body
//	@Failure		409	{object}	apperror.Body	"QUOTA_FULL, ALREADY_REGISTERED, EVENT_ENDED"
//	@Router			/api/v1/events/{id}/register [post]
func (h *EventHandler) Register(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	reg, err := h.events.Register(c.Request.Context(), actor(c), id)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	respond(c, http.StatusCreated, toRegistrationResponse(reg))
}

// Cancel cancels the caller's registration.
//
//	@Summary		Batalkan pendaftaran
//	@Description	Slot kuota dikembalikan. Ditolak jika event sudah selesai atau sudah check-in.
//	@Tags			registrations
//	@Security		BearerAuth
//	@Param			id	path	string	true	"ID event (UUID)"
//	@Success		204
//	@Failure		401	{object}	apperror.Body
//	@Failure		404	{object}	apperror.Body
//	@Failure		409	{object}	apperror.Body	"EVENT_ENDED, ALREADY_CHECKED_IN"
//	@Router			/api/v1/events/{id}/register [delete]
func (h *EventHandler) Cancel(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	if err := h.events.CancelRegistration(c.Request.Context(), actor(c), id); err != nil {
		apperror.Respond(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Participants lists an event's participants.
//
//	@Summary		Daftar peserta event
//	@Description	Lead: semua peserta. Division Head: semua peserta event divisinya, dan hanya anggota divisinya di event lain. Member: hanya data dirinya.
//	@Tags			registrations
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id		path		string	true	"ID event (UUID)"
//	@Param			page	query		int		false	"Halaman (mulai 1)"				default(1)
//	@Param			limit	query		int		false	"Jumlah per halaman (maks 100)"	default(20)
//	@Param			status	query		string	false	"Status pendaftaran"			Enums(registered, cancelled)	default(registered)
//	@Success		200		{object}	pageResponse{data=[]ParticipantResponse}
//	@Failure		400		{object}	apperror.Body
//	@Failure		401		{object}	apperror.Body
//	@Failure		404		{object}	apperror.Body
//	@Router			/api/v1/events/{id}/participants [get]
func (h *EventHandler) Participants(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	var q listParticipantsQuery
	if !bindQuery(c, &q) {
		return
	}
	status := domain.RegistrationStatus(q.Status)
	page, err := h.events.ListParticipants(c.Request.Context(), actor(c), domain.ParticipantFilter{
		EventID: id, Status: &status, Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	items := make([]ParticipantResponse, 0, len(page.Items))
	for _, p := range page.Items {
		items = append(items, ParticipantResponse{
			RegistrationID: p.ID,
			User: ParticipantUser{
				ID: p.UserID, Name: p.UserName, Email: p.UserEmail,
				Division: divisionRef(p.DivisionID, p.DivisionName),
			},
			Status:       p.Status,
			RegisteredAt: p.CreatedAt,
			UpdatedAt:    p.UpdatedAt,
		})
	}
	c.JSON(http.StatusOK, pageResponse{Data: items, Meta: toPageMeta(page)})
}
