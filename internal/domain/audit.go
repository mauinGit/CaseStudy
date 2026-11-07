package domain

import "github.com/google/uuid"

// Audit actions. Every entry is written in the same transaction as the change
// it describes, so a change can never be committed without its audit trail.
const (
	AuditMemberRoleChanged = "member.role_changed"
	AuditMemberDeleted     = "member.deleted"
)

// Audited entity types.
const (
	EntityUser = "user"
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
