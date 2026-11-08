package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mauinGit/gdgoc-core-team-api/internal/apperror"
	"github.com/mauinGit/gdgoc-core-team-api/internal/service"
)

type UserHandler struct {
	users *service.UserService
}

func NewUserHandler(users *service.UserService) *UserHandler {
	return &UserHandler{users: users}
}

// GetMe returns the authenticated user's profile.
//
//	@Summary	Profil user yang sedang login
//	@Tags		me
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	dataResponse{data=UserResponse}
//	@Failure	401	{object}	apperror.Body
//	@Router		/api/v1/me [get]
func (h *UserHandler) GetMe(c *gin.Context) {
	u, err := h.users.GetMe(c.Request.Context(), actor(c))
	if err != nil {
		apperror.Respond(c, err)
		return
	}
	respond(c, http.StatusOK, toUserResponse(u))
}
