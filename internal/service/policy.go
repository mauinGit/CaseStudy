package service

import "github.com/mauinGit/gdgoc-core-team-api/internal/domain"

// Authorization rules for member data. They combine the actor's role with
// data ownership (division / self) and are always evaluated in the service
// layer, against rows loaded from the database.
//
//	              view                  create / edit / delete
//	Lead          everyone              everyone (except deleting self)
//	Div Head      own division + self   role=member in own division
//	Member        self                  nobody (own profile via /me)

// canViewMember reports whether actor may read target.
func canViewMember(a domain.Actor, t *domain.User) bool {
	if a.UserID == t.ID {
		return true
	}
	switch a.Role {
	case domain.RoleLead:
		return true
	case domain.RoleDivisionHead:
		return a.InDivision(t.DivisionID)
	default:
		return false
	}
}

// canManageMember reports whether actor may edit or delete target.
func canManageMember(a domain.Actor, t *domain.User) bool {
	switch a.Role {
	case domain.RoleLead:
		return true
	case domain.RoleDivisionHead:
		// Division Heads manage plain members of their own division only,
		// never Leads or other Division Heads.
		return t.Role == domain.RoleMember && a.InDivision(t.DivisionID)
	default:
		return false
	}
}

// canManageMembers reports whether actor's role may manage members at all.
func canManageMembers(a domain.Actor) bool {
	return a.IsLead() || a.IsDivisionHead()
}
