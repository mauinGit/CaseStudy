package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
)

type AuditRepo struct {
	db DBTX
}

// Insert appends an audit entry. Call it with the transaction's Repos so the
// entry commits (or rolls back) together with the change it records.
func (r *AuditRepo) Insert(ctx context.Context, e domain.AuditEntry) error {
	oldVal, err := jsonOrNil(e.OldValue)
	if err != nil {
		return fmt.Errorf("audit old value: %w", err)
	}
	newVal, err := jsonOrNil(e.NewValue)
	if err != nil {
		return fmt.Errorf("audit new value: %w", err)
	}
	// Store the IP only if it parses, so a malformed value never fails the write.
	var ip *string
	if addr, err := netip.ParseAddr(e.IP); err == nil {
		s := addr.String()
		ip = &s
	}

	_, err = r.db.Exec(ctx, `
INSERT INTO audit_logs (actor_id, action, entity, entity_id, old_value, new_value, ip)
VALUES ($1, $2, $3, $4, $5, $6, $7::inet)`,
		e.ActorID, e.Action, e.Entity, e.EntityID, oldVal, newVal, ip)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

func jsonOrNil(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

// List returns a page of audit logs, newest first, with the actor's name.
func (r *AuditRepo) List(ctx context.Context, f domain.AuditFilter) (domain.Page[*domain.AuditLog], error) {
	where := []string{"TRUE"}
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.Action != "" {
		where = append(where, "l.action = "+arg(f.Action))
	}
	if f.Entity != "" {
		where = append(where, "l.entity = "+arg(f.Entity))
	}
	if f.EntityID != "" {
		where = append(where, "l.entity_id = "+arg(f.EntityID))
	}
	if f.ActorID != nil {
		where = append(where, "l.actor_id = "+arg(*f.ActorID))
	}
	cond := " WHERE " + strings.Join(where, " AND ")

	page := domain.Page[*domain.AuditLog]{Items: []*domain.AuditLog{}, Page: f.Page, Limit: f.Limit}
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM audit_logs l`+cond, args...).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("count audit logs: %w", err)
	}
	if page.Total == 0 || f.Offset() >= page.Total {
		return page, nil
	}

	query := `
SELECT l.id, l.actor_id, u.name, l.action, l.entity, l.entity_id, l.old_value, l.new_value, host(l.ip), l.created_at
FROM audit_logs l LEFT JOIN users u ON u.id = l.actor_id` + cond +
		fmt.Sprintf(" ORDER BY l.created_at DESC, l.id DESC LIMIT %s OFFSET %s", arg(f.Limit), arg(f.Offset()))
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return page, fmt.Errorf("list audit logs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var l domain.AuditLog
		if err := rows.Scan(&l.ID, &l.ActorID, &l.ActorName, &l.Action, &l.Entity, &l.EntityID,
			&l.OldValue, &l.NewValue, &l.IP, &l.CreatedAt); err != nil {
			return page, fmt.Errorf("list audit logs: %w", err)
		}
		page.Items = append(page.Items, &l)
	}
	return page, wrap("list audit logs", rows.Err())
}
