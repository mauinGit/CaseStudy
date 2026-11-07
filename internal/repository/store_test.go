package repository_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
	"github.com/mauinGit/gdgoc-core-team-api/internal/repository"
	"github.com/mauinGit/gdgoc-core-team-api/internal/testutil"
	"github.com/mauinGit/gdgoc-core-team-api/seed"
)

// If writing the audit entry fails, the change it describes must roll back
// too: both live in one transaction.
func TestInTxRollsBackChangeWhenAuditFails(t *testing.T) {
	pool := testutil.NewDB(t)
	testutil.Seed(t, pool)
	store := repository.NewStore(pool)
	ctx := context.Background()
	id := uuid.MustParse(seed.UserMemberBE1)

	// The database itself refuses a non-lead without a division.
	err := store.InTx(ctx, func(r repository.Repos) error {
		return r.Users.UpdateRole(ctx, id, domain.RoleDivisionHead, nil)
	})
	require.ErrorIs(t, err, domain.ErrDivisionRequired, "CHECK constraint is mapped to a domain error")

	div := uuid.MustParse(seed.DivBackend)
	err = store.InTx(ctx, func(r repository.Repos) error {
		if err := r.Users.UpdateRole(ctx, id, domain.RoleDivisionHead, &div); err != nil {
			return err
		}
		// action is VARCHAR(64): this insert fails inside the transaction.
		return r.Audit.Insert(ctx, domain.AuditEntry{
			Action: strings.Repeat("x", 65), Entity: domain.EntityUser, EntityID: id.String(),
		})
	})
	require.Error(t, err)

	u, err := store.Users.GetActiveByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.RoleMember, u.Role, "role change rolled back with the failed audit write")

	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs`).Scan(&n))
	assert.Zero(t, n)
}
