package service

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
	"github.com/mauinGit/gdgoc-core-team-api/internal/repository"
)

var errMemberNotFound = domain.ErrNotFound.WithMessage("Anggota tidak ditemukan")

// UserService manages member accounts and profiles.
type UserService struct {
	store      *repository.Store
	bcryptCost int
}

func NewUserService(store *repository.Store, bcryptCost int) *UserService {
	return &UserService{store: store, bcryptCost: bcryptCost}
}

// ProfilePatch holds the fields a user may change on their own profile.
// nil means "unchanged"; an empty Phone/Bio clears the field.
type ProfilePatch struct {
	Name  *string
	Phone *string
	Bio   *string
}

func (p ProfilePatch) empty() bool { return p.Name == nil && p.Phone == nil && p.Bio == nil }

func (p ProfilePatch) apply(u *domain.User) {
	if p.Name != nil {
		u.Name = strings.TrimSpace(*p.Name)
	}
	if p.Phone != nil {
		u.Phone = emptyToNil(*p.Phone)
	}
	if p.Bio != nil {
		u.Bio = emptyToNil(*p.Bio)
	}
}

// MemberPatch holds the fields a Lead/Division Head may change on a member.
// Role and division are changed through ChangeRole only.
type MemberPatch struct {
	ProfilePatch
	Email *string
}

// CreateMemberInput is a new member account.
type CreateMemberInput struct {
	Name       string
	Email      string
	Password   *string // nil = Google-only account
	Role       domain.Role
	DivisionID *uuid.UUID
	Phone      *string
	Bio        *string
}

// RoleChange is a new role (and optionally division) for a member.
type RoleChange struct {
	Role       domain.Role
	DivisionID *uuid.UUID // nil = keep the current division
}

// GetMe returns the profile of the authenticated user.
func (s *UserService) GetMe(ctx context.Context, actor domain.Actor) (*domain.User, error) {
	return s.store.Users.GetActiveByID(ctx, actor.UserID)
}

// UpdateMe updates the caller's own profile. Only safe fields are accepted
// (see ProfilePatch), so role, division and deleted_at cannot be changed.
func (s *UserService) UpdateMe(ctx context.Context, actor domain.Actor, p ProfilePatch) (*domain.User, error) {
	var out *domain.User
	err := s.store.InTx(ctx, func(r repository.Repos) error {
		locked, err := r.Users.LockActive(ctx, actor.UserID)
		if err != nil {
			return err
		}
		me, ok := locked[actor.UserID]
		if !ok {
			return errMemberNotFound
		}
		if !p.empty() {
			p.apply(me)
			if err := r.Users.UpdateProfile(ctx, me); err != nil {
				return err
			}
		}
		out, err = r.Users.GetActiveByID(ctx, me.ID)
		return err
	})
	return out, err
}

// ListMembers returns the members visible to the actor. The scope is forced
// from the actor's role: Division Heads only see their division and Members
// only see themselves, whatever filters they send.
func (s *UserService) ListMembers(ctx context.Context, actor domain.Actor, f domain.MemberFilter) (domain.Page[*domain.User], error) {
	switch {
	case actor.IsLead():
	case actor.IsDivisionHead() && actor.DivisionID != nil:
		if f.DivisionID != nil && !actor.InDivision(f.DivisionID) {
			return domain.Page[*domain.User]{}, domain.ErrForbidden.WithMessage("Anda hanya dapat melihat anggota divisi Anda sendiri")
		}
		f.DivisionID = actor.DivisionID
	default:
		f.OnlyUserID = &actor.UserID
	}
	return s.store.Users.List(ctx, f)
}

// GetMember returns one member. Members outside the actor's scope are
// reported as not found so their existence is not revealed (IDOR).
func (s *UserService) GetMember(ctx context.Context, actor domain.Actor, id uuid.UUID) (*domain.User, error) {
	u, err := s.store.Users.GetActiveByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !canViewMember(actor, u) {
		return nil, errMemberNotFound
	}
	return u, nil
}

// CreateMember registers a new member account (Lead or Division Head only).
func (s *UserService) CreateMember(ctx context.Context, actor domain.Actor, in CreateMemberInput) (*domain.User, error) {
	if in.Role == "" {
		in.Role = domain.RoleMember
	}
	switch {
	case actor.IsLead():
	case actor.IsDivisionHead() && actor.DivisionID != nil:
		if in.Role != domain.RoleMember {
			return nil, domain.ErrForbidden.WithMessage("Division Head hanya dapat menambahkan anggota dengan role member")
		}
		if in.DivisionID == nil {
			in.DivisionID = actor.DivisionID
		} else if !actor.InDivision(in.DivisionID) {
			return nil, domain.ErrForbidden.WithMessage("Anda hanya dapat menambahkan anggota ke divisi Anda sendiri")
		}
	default:
		return nil, domain.ErrForbidden
	}
	if in.Role != domain.RoleLead && in.DivisionID == nil {
		return nil, domain.ErrDivisionRequired
	}

	u := &domain.User{
		Name:       strings.TrimSpace(in.Name),
		Email:      NormalizeEmail(in.Email),
		Role:       in.Role,
		DivisionID: in.DivisionID,
	}
	if in.Phone != nil {
		u.Phone = emptyToNil(*in.Phone)
	}
	if in.Bio != nil {
		u.Bio = emptyToNil(*in.Bio)
	}
	if in.Password != nil {
		hash, err := s.hashPassword(*in.Password)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = &hash
	}

	id, err := s.store.Users.Create(ctx, u)
	if err != nil {
		return nil, err
	}
	return s.store.Users.GetActiveByID(ctx, id)
}

// UpdateMember edits a member's profile fields (Lead: anyone, Division Head:
// members of their own division).
func (s *UserService) UpdateMember(ctx context.Context, actor domain.Actor, id uuid.UUID, p MemberPatch) (*domain.User, error) {
	if !canManageMembers(actor) {
		return nil, domain.ErrForbidden
	}
	var out *domain.User
	err := s.withLocked(ctx, actor, id, func(r repository.Repos, a domain.Actor, t *domain.User) error {
		if !canManageMember(a, t) {
			return domain.ErrForbidden.WithMessage("Anda tidak dapat mengubah data anggota ini")
		}
		if !p.empty() || p.Email != nil {
			p.apply(t)
			if p.Email != nil {
				t.Email = NormalizeEmail(*p.Email)
			}
			if err := r.Users.UpdateProfile(ctx, t); err != nil {
				return err
			}
		}
		var err error
		out, err = r.Users.GetActiveByID(ctx, t.ID)
		return err
	})
	return out, err
}

type roleSnapshot struct {
	Role       domain.Role `json:"role"`
	DivisionID *uuid.UUID  `json:"division_id"`
}

// ChangeRole changes a member's role (Lead only). The change and its audit
// entry are committed in one transaction.
func (s *UserService) ChangeRole(ctx context.Context, actor domain.Actor, id uuid.UUID, in RoleChange) (*domain.User, error) {
	if !actor.IsLead() {
		return nil, domain.ErrForbidden.WithMessage("Hanya Lead yang dapat mengubah role anggota")
	}
	// A Lead cannot demote themselves, so there is always at least one Lead.
	if actor.UserID == id {
		return nil, domain.ErrCannotChangeOwnRole
	}

	var out *domain.User
	err := s.withLocked(ctx, actor, id, func(r repository.Repos, a domain.Actor, t *domain.User) error {
		if !a.IsLead() {
			return domain.ErrForbidden.WithMessage("Hanya Lead yang dapat mengubah role anggota")
		}
		division := t.DivisionID
		if in.DivisionID != nil {
			division = in.DivisionID
		}
		if in.Role != domain.RoleLead && division == nil {
			return domain.ErrDivisionRequired
		}

		before := roleSnapshot{Role: t.Role, DivisionID: t.DivisionID}
		after := roleSnapshot{Role: in.Role, DivisionID: division}
		if before.Role != after.Role || !sameUUID(before.DivisionID, after.DivisionID) {
			if err := r.Users.UpdateRole(ctx, t.ID, in.Role, division); err != nil {
				return err
			}
			if err := r.Audit.Insert(ctx, domain.AuditEntry{
				ActorID:  &a.UserID,
				Action:   domain.AuditMemberRoleChanged,
				Entity:   domain.EntityUser,
				EntityID: t.ID.String(),
				OldValue: before,
				NewValue: after,
				IP:       a.IP,
			}); err != nil {
				return err
			}
		}
		var err error
		out, err = r.Users.GetActiveByID(ctx, t.ID)
		return err
	})
	return out, err
}

type memberSnapshot struct {
	Name       string      `json:"name"`
	Email      string      `json:"email"`
	Role       domain.Role `json:"role"`
	DivisionID *uuid.UUID  `json:"division_id"`
}

// DeleteMember soft-deletes a member and records it in the audit log within
// the same transaction.
func (s *UserService) DeleteMember(ctx context.Context, actor domain.Actor, id uuid.UUID) error {
	if !canManageMembers(actor) {
		return domain.ErrForbidden
	}
	if actor.UserID == id {
		return domain.ErrCannotDeleteSelf
	}
	return s.withLocked(ctx, actor, id, func(r repository.Repos, a domain.Actor, t *domain.User) error {
		if !canManageMember(a, t) {
			return domain.ErrForbidden.WithMessage("Anda tidak dapat menghapus anggota ini")
		}
		if err := r.Users.SoftDelete(ctx, t.ID); err != nil {
			return err
		}
		return r.Audit.Insert(ctx, domain.AuditEntry{
			ActorID:  &a.UserID,
			Action:   domain.AuditMemberDeleted,
			Entity:   domain.EntityUser,
			EntityID: t.ID.String(),
			OldValue: memberSnapshot{Name: t.Name, Email: t.Email, Role: t.Role, DivisionID: t.DivisionID},
			IP:       a.IP,
		})
	})
}

// ListDivisions returns every division.
func (s *UserService) ListDivisions(ctx context.Context) ([]domain.Division, error) {
	return s.store.Divisions.List(ctx)
}

// withLocked runs fn in a transaction after locking both the actor's and the
// target's rows. The actor's role is re-read from the locked row, so the
// authorization decision cannot race with a concurrent role change or delete
// (e.g. two Leads demoting each other at the same time).
func (s *UserService) withLocked(ctx context.Context, actor domain.Actor, targetID uuid.UUID,
	fn func(r repository.Repos, a domain.Actor, target *domain.User) error,
) error {
	return s.store.InTx(ctx, func(r repository.Repos) error {
		locked, err := r.Users.LockActive(ctx, actor.UserID, targetID)
		if err != nil {
			return err
		}
		me, ok := locked[actor.UserID]
		if !ok {
			return domain.ErrInvalidToken
		}
		fresh := domain.Actor{UserID: me.ID, Role: me.Role, DivisionID: me.DivisionID, IP: actor.IP}
		if !canManageMembers(fresh) {
			return domain.ErrForbidden
		}
		target, ok := locked[targetID]
		if !ok || !canViewMember(fresh, target) {
			return errMemberNotFound
		}
		return fn(r, fresh, target)
	})
}

func (s *UserService) hashPassword(pw string) (string, error) {
	if len(pw) > 72 {
		return "", domain.ErrPasswordTooLong
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), s.bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func emptyToNil(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
