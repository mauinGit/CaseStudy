package repository

import (
	"context"
	"fmt"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
)

type DivisionRepo struct {
	db DBTX
}

// List returns all divisions ordered by name.
func (r *DivisionRepo) List(ctx context.Context) ([]domain.Division, error) {
	rows, err := r.db.Query(ctx, `SELECT id, name, created_at FROM divisions ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list divisions: %w", err)
	}
	defer rows.Close()

	out := []domain.Division{}
	for rows.Next() {
		var d domain.Division
		if err := rows.Scan(&d.ID, &d.Name, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("list divisions: %w", err)
		}
		out = append(out, d)
	}
	return out, wrap("list divisions", rows.Err())
}
