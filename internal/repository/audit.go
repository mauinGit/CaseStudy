package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"

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
