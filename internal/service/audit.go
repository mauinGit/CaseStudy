package service

import (
	"context"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
	"github.com/mauinGit/gdgoc-core-team-api/internal/repository"
)

// AuditService exposes the audit log.
type AuditService struct {
	store *repository.Store
}

func NewAuditService(store *repository.Store) *AuditService {
	return &AuditService{store: store}
}

// List returns audit logs, newest first. Lead only.
func (s *AuditService) List(ctx context.Context, actor domain.Actor, f domain.AuditFilter) (domain.Page[*domain.AuditLog], error) {
	if !actor.IsLead() {
		return domain.Page[*domain.AuditLog]{}, domain.ErrForbidden.WithMessage("Hanya Lead yang dapat melihat audit log")
	}
	return s.store.Audit.List(ctx, f)
}
