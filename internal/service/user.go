package service

import (
	"context"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
	"github.com/mauinGit/gdgoc-core-team-api/internal/repository"
)

// UserService manages member accounts and profiles.
type UserService struct {
	store *repository.Store
}

func NewUserService(store *repository.Store) *UserService {
	return &UserService{store: store}
}

// GetMe returns the profile of the authenticated user.
func (s *UserService) GetMe(ctx context.Context, actor domain.Actor) (*domain.User, error) {
	return s.store.Users.GetActiveByID(ctx, actor.UserID)
}
