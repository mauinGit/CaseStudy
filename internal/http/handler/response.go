package handler

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
	"github.com/mauinGit/gdgoc-core-team-api/internal/http/middleware"
)

// dataResponse is the success envelope: { "data": ... }.
type dataResponse struct {
	Data any `json:"data"`
}

func respond(c *gin.Context, status int, data any) {
	c.JSON(status, dataResponse{Data: data})
}

// actor returns the authenticated actor; routes using it are always behind
// the Authenticate middleware.
func actor(c *gin.Context) domain.Actor {
	a, _ := middleware.ActorFrom(c)
	return a
}

// DivisionRef is an embedded division reference.
type DivisionRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// UserResponse is the public representation of a user. It never includes
// password_hash or google_sub.
type UserResponse struct {
	ID           uuid.UUID    `json:"id"`
	Name         string       `json:"name"`
	Email        string       `json:"email"`
	Role         domain.Role  `json:"role"`
	Division     *DivisionRef `json:"division"`
	Phone        *string      `json:"phone"`
	Bio          *string      `json:"bio"`
	GoogleLinked bool         `json:"google_linked"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

func toUserResponse(u *domain.User) UserResponse {
	r := UserResponse{
		ID:           u.ID,
		Name:         u.Name,
		Email:        u.Email,
		Role:         u.Role,
		Phone:        u.Phone,
		Bio:          u.Bio,
		GoogleLinked: u.GoogleSub != nil,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
	}
	r.Division = divisionRef(u.DivisionID, u.DivisionName)
	return r
}
