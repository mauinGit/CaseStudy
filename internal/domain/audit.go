package domain

import (
	"time"

	"github.com/google/uuid"
)

// Audit actions. Every entry is written in the same transaction as the change
// it describes, so a change can never be committed without its audit trail.
const (
	AuditMemberRoleChanged = "member.role_changed"
	AuditMemberDeleted     = "member.deleted"
	AuditEventQuotaChanged = "event.quota_changed"
)

// Audited entity types.
const (
	EntityUser  = "user"
	EntityEvent = "event"
)

// AuditEntry is a new audit log row. OldValue/NewValue are JSON-encoded.
type AuditEntry struct {
	ActorID  *uuid.UUID
	Action   string
	Entity   string
	EntityID string
	OldValue any
	NewValue any
	IP       string
}

// AuditLog is a stored audit log row.
type AuditLog struct {
	ID        int64
	ActorID   *uuid.UUID
	ActorName *string
	Action    string
	Entity    string
	EntityID  string
	OldValue  []byte // raw JSON, nil when absent
	NewValue  []byte
	IP        *string
	CreatedAt time.Time
}

// AuditFilter describes an audit log list query (newest first).
type AuditFilter struct {
	Action   string
	Entity   string
	EntityID string
	ActorID  *uuid.UUID
	Page     int
	Limit    int
}

func (f AuditFilter) Offset() int { return (f.Page - 1) * f.Limit }
