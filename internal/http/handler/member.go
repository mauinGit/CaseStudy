package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
	"github.com/mauinGit/gdgoc-core-team-api/internal/service"
)

// ---- request DTOs ----
// Each endpoint has its own DTO that declares only the fields it accepts.
// Unknown fields such as "role", "division_id" or "deleted_at" in a profile
// update are never read, which prevents mass assignment.

type listMembersQuery struct {
	Page       int    `form:"page,default=1" binding:"min=1,max=100000"`
	Limit      int    `form:"limit,default=20" binding:"min=1,max=100"`
	Role       string `form:"role" binding:"omitempty,oneof=lead division_head member"`
	DivisionID string `form:"division_id" binding:"omitempty,uuid"`
	Search     string `form:"search" binding:"max=100"`
	Sort       string `form:"sort,default=name" binding:"oneof=name created_at email"`
	Order      string `form:"order,default=asc" binding:"oneof=asc desc"`
}

type createMemberRequest struct {
	Name       string  `json:"name" binding:"required,notblank,min=2,max=100"`
	Email      string  `json:"email" binding:"required,email,max=254"`
	Password   *string `json:"password" binding:"omitempty,min=8,max=72"`
	Role       string  `json:"role" binding:"omitempty,oneof=lead division_head member"`
	DivisionID *string `json:"division_id" binding:"omitempty,uuid"`
	Phone      *string `json:"phone" binding:"omitempty,phone"`
	Bio        *string `json:"bio" binding:"omitempty,max=500"`
}

// updateProfileRequest is used by PATCH /me. Only safe fields are declared.
type updateProfileRequest struct {
	Name  *string `json:"name" binding:"omitempty,notblank,min=2,max=100"`
	Phone *string `json:"phone" binding:"omitempty,phone"`
	Bio   *string `json:"bio" binding:"omitempty,max=500"`
}

// updateMemberRequest is used by PATCH /members/{id}. Role and division are
// changed through PATCH /members/{id}/role only.
type updateMemberRequest struct {
	updateProfileRequest
	Email *string `json:"email" binding:"omitempty,email,max=254"`
}

type changeRoleRequest struct {
	Role       string  `json:"role" binding:"required,oneof=lead division_head member"`
	DivisionID *string `json:"division_id" binding:"omitempty,uuid"`
}

type idParam struct {
	ID string `uri:"id" binding:"required,uuid"`
}

func (r updateProfileRequest) patch() service.ProfilePatch {
	return service.ProfilePatch{Name: r.Name, Phone: r.Phone, Bio: r.Bio}
}

// ---- responses ----

type pageMeta struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type pageResponse struct {
	Data any      `json:"data"`
	Meta pageMeta `json:"meta"`
}

func toPageMeta[T any](p domain.Page[T]) pageMeta {
	return pageMeta{Page: p.Page, Limit: p.Limit, Total: p.Total, TotalPages: p.TotalPages()}
}

type DivisionResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// ---- handlers ----

// UpdateMe updates the caller's own profile (name, phone, bio).
//
//	@Summary		Ubah profil sendiri
//	@Description	Hanya field aman (name, phone, bio). Field lain seperti role, division_id, deleted_at diabaikan. String kosong menghapus phone/bio.
//	@Tags			me
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		updateProfileRequest	true	"Field yang diubah"
//	@Success		200		{object}	dataResponse{data=UserResponse}
//	@Failure		400		{object}	apperror.Body
//	@Failure		401		{object}	apperror.Body
//	@Router			/api/v1/me [patch]
func (h *UserHandler) UpdateMe(c *gin.Context) {
	var req updateProfileRequest
	if !bindJSON(c, &req) {
		return
	}
	u, err := h.users.UpdateMe(c.Request.Context(), actor(c), req.patch())
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	respond(c, http.StatusOK, toUserResponse(u))
}

// ListMembers lists members visible to the caller.
//
//	@Summary		Daftar anggota
//	@Description	Lead melihat semua anggota, Division Head hanya divisinya, Member hanya dirinya sendiri.
//	@Tags			members
//	@Security		BearerAuth
//	@Produce		json
//	@Param			page		query		int		false	"Halaman (mulai 1)"					default(1)
//	@Param			limit		query		int		false	"Jumlah per halaman (maks 100)"		default(20)
//	@Param			role		query		string	false	"Filter role"						Enums(lead, division_head, member)
//	@Param			division_id	query		string	false	"Filter divisi (UUID)"
//	@Param			search		query		string	false	"Cari nama/email (case-insensitive)"
//	@Param			sort		query		string	false	"Kolom urutan"						Enums(name, created_at, email)	default(name)
//	@Param			order		query		string	false	"Arah urutan"						Enums(asc, desc)				default(asc)
//	@Success		200			{object}	pageResponse{data=[]UserResponse}
//	@Failure		400			{object}	apperror.Body
//	@Failure		401			{object}	apperror.Body
//	@Failure		403			{object}	apperror.Body
//	@Router			/api/v1/members [get]
func (h *UserHandler) ListMembers(c *gin.Context) {
	var q listMembersQuery
	if !bindQuery(c, &q) {
		return
	}
	f := domain.MemberFilter{
		Search: strings.TrimSpace(q.Search),
		Sort:   domain.MemberSort(q.Sort),
		Desc:   q.Order == "desc",
		Page:   q.Page,
		Limit:  q.Limit,
	}
	if q.Role != "" {
		role := domain.Role(q.Role)
		f.Role = &role
	}
	if q.DivisionID != "" {
		id := uuid.MustParse(q.DivisionID) // validated by binding
		f.DivisionID = &id
	}

	page, err := h.users.ListMembers(c.Request.Context(), actor(c), f)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	items := make([]UserResponse, 0, len(page.Items))
	for _, u := range page.Items {
		items = append(items, toUserResponse(u))
	}
	c.JSON(http.StatusOK, pageResponse{Data: items, Meta: toPageMeta(page)})
}

// GetMember returns one member.
//
//	@Summary		Detail anggota
//	@Description	Anggota di luar akses pemanggil dilaporkan 404 agar keberadaannya tidak bocor.
//	@Tags			members
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		string	true	"ID anggota (UUID)"
//	@Success		200	{object}	dataResponse{data=UserResponse}
//	@Failure		400	{object}	apperror.Body
//	@Failure		401	{object}	apperror.Body
//	@Failure		404	{object}	apperror.Body
//	@Router			/api/v1/members/{id} [get]
func (h *UserHandler) GetMember(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	u, err := h.users.GetMember(c.Request.Context(), actor(c), id)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	respond(c, http.StatusOK, toUserResponse(u))
}

// CreateMember registers a new member account.
//
//	@Summary		Tambah anggota
//	@Description	Lead dapat menambah role apa pun. Division Head hanya menambah role member di divisinya (division_id default ke divisinya). Tanpa password, akun hanya bisa login via Google.
//	@Tags			members
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		createMemberRequest	true	"Data anggota"
//	@Success		201		{object}	dataResponse{data=UserResponse}
//	@Failure		400		{object}	apperror.Body
//	@Failure		401		{object}	apperror.Body
//	@Failure		403		{object}	apperror.Body
//	@Failure		409		{object}	apperror.Body
//	@Failure		422		{object}	apperror.Body
//	@Router			/api/v1/members [post]
func (h *UserHandler) CreateMember(c *gin.Context) {
	var req createMemberRequest
	if !bindJSON(c, &req) {
		return
	}
	in := service.CreateMemberInput{
		Name:       req.Name,
		Email:      req.Email,
		Password:   req.Password,
		Role:       domain.Role(req.Role),
		DivisionID: parseOptionalUUID(req.DivisionID),
		Phone:      req.Phone,
		Bio:        req.Bio,
	}
	u, err := h.users.CreateMember(c.Request.Context(), actor(c), in)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	c.Header("Location", "/api/v1/members/"+u.ID.String())
	respond(c, http.StatusCreated, toUserResponse(u))
}

// UpdateMember edits a member's profile.
//
//	@Summary		Ubah data anggota
//	@Description	Lead: semua anggota. Division Head: anggota (role member) di divisinya. Role/divisi diubah lewat endpoint /role.
//	@Tags			members
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"ID anggota (UUID)"
//	@Param			body	body		updateMemberRequest	true	"Field yang diubah"
//	@Success		200		{object}	dataResponse{data=UserResponse}
//	@Failure		400		{object}	apperror.Body
//	@Failure		401		{object}	apperror.Body
//	@Failure		403		{object}	apperror.Body
//	@Failure		404		{object}	apperror.Body
//	@Failure		409		{object}	apperror.Body
//	@Router			/api/v1/members/{id} [patch]
func (h *UserHandler) UpdateMember(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	var req updateMemberRequest
	if !bindJSON(c, &req) {
		return
	}
	p := service.MemberPatch{ProfilePatch: req.patch(), Email: req.Email}
	u, err := h.users.UpdateMember(c.Request.Context(), actor(c), id, p)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	respond(c, http.StatusOK, toUserResponse(u))
}

// ChangeRole changes a member's role (Lead only, audited).
//
//	@Summary		Ubah role anggota
//	@Description	Hanya Lead. Tercatat di audit log dalam transaksi yang sama. division_id opsional (default: divisi saat ini), wajib ada untuk role selain lead.
//	@Tags			members
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"ID anggota (UUID)"
//	@Param			body	body		changeRoleRequest	true	"Role baru"
//	@Success		200		{object}	dataResponse{data=UserResponse}
//	@Failure		400		{object}	apperror.Body
//	@Failure		401		{object}	apperror.Body
//	@Failure		403		{object}	apperror.Body
//	@Failure		404		{object}	apperror.Body
//	@Failure		422		{object}	apperror.Body
//	@Router			/api/v1/members/{id}/role [patch]
func (h *UserHandler) ChangeRole(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	var req changeRoleRequest
	if !bindJSON(c, &req) {
		return
	}
	in := service.RoleChange{Role: domain.Role(req.Role), DivisionID: parseOptionalUUID(req.DivisionID)}
	u, err := h.users.ChangeRole(c.Request.Context(), actor(c), id, in)
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	respond(c, http.StatusOK, toUserResponse(u))
}

// DeleteMember soft-deletes a member (audited).
//
//	@Summary		Hapus anggota (soft delete)
//	@Description	Lead: semua anggota kecuali diri sendiri. Division Head: anggota (role member) di divisinya. Anggota yang dihapus tidak bisa login dan tokennya langsung tidak berlaku.
//	@Tags			members
//	@Security		BearerAuth
//	@Param			id	path	string	true	"ID anggota (UUID)"
//	@Success		204
//	@Failure		400	{object}	apperror.Body
//	@Failure		401	{object}	apperror.Body
//	@Failure		403	{object}	apperror.Body
//	@Failure		404	{object}	apperror.Body
//	@Router			/api/v1/members/{id} [delete]
func (h *UserHandler) DeleteMember(c *gin.Context) {
	id, ok := bindID(c)
	if !ok {
		return
	}
	if err := h.users.DeleteMember(c.Request.Context(), actor(c), id); err != nil {
		apperror.Respond(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListDivisions returns every division.
//
//	@Summary	Daftar divisi
//	@Tags		divisions
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	dataResponse{data=[]DivisionResponse}
//	@Failure	401	{object}	apperror.Body
//	@Router		/api/v1/divisions [get]
func (h *UserHandler) ListDivisions(c *gin.Context) {
	divs, err := h.users.ListDivisions(c.Request.Context())
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	out := make([]DivisionResponse, 0, len(divs))
	for _, d := range divs {
		out = append(out, DivisionResponse{ID: d.ID, Name: d.Name})
	}
	respond(c, http.StatusOK, out)
}

// bindID validates the :id path parameter.
func bindID(c *gin.Context) (uuid.UUID, bool) {
	var p idParam
	if err := c.ShouldBindUri(&p); err != nil {
		respondBindError(c, err)
		return uuid.Nil, false
	}
	return uuid.MustParse(p.ID), true
}

// parseOptionalUUID parses an already-validated optional UUID.
func parseOptionalUUID(s *string) *uuid.UUID {
	if s == nil {
		return nil
	}
	id := uuid.MustParse(*s)
	return &id
}
