package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mauinGit/gdgoc-core-team-api/internal/testutil"
	"github.com/mauinGit/gdgoc-core-team-api/seed"
)

type listBody struct {
	Data []userBody `json:"data"`
	Meta struct {
		Page       int `json:"page"`
		Limit      int `json:"limit"`
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
	} `json:"meta"`
}

func listMembers(t *testing.T, api *testutil.API, token, query string) listBody {
	t.Helper()
	rec := api.Do(http.MethodGet, "/api/v1/members"+query, token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body listBody
	testutil.DecodeJSON(t, rec, &body)
	return body
}

func userFrom(t *testing.T, api *testutil.API, method, path, token string, body any, wantStatus int) userBody {
	t.Helper()
	rec := api.Do(method, path, token, body)
	require.Equal(t, wantStatus, rec.Code, rec.Body.String())
	var res struct {
		Data userBody `json:"data"`
	}
	testutil.DecodeJSON(t, rec, &res)
	return res.Data
}

func memberPath(id string) string { return "/api/v1/members/" + id }

func emails(users []userBody) []string {
	out := make([]string, len(users))
	for i, u := range users {
		out[i] = u.Email
	}
	return out
}

// auditCount returns the number of audit rows for an action on an entity.
func auditCount(t *testing.T, api *testutil.API, action, entityID string) int {
	t.Helper()
	var n int
	require.NoError(t, api.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_logs WHERE action = $1 AND entity_id = $2`, action, entityID).Scan(&n))
	return n
}

// ---------------------------------------------------------------- listing --

func TestListMembersIsScopedByRole(t *testing.T) {
	api := testutil.NewAPI(t)

	t.Run("lead sees everyone", func(t *testing.T) {
		body := listMembers(t, api, api.Login(emailLead), "")
		assert.Equal(t, 8, body.Meta.Total)
	})

	t.Run("division head sees only own division", func(t *testing.T) {
		token := api.Login(emailHeadBE)
		body := listMembers(t, api, token, "")
		assert.Equal(t, 3, body.Meta.Total)
		for _, u := range body.Data {
			require.NotNil(t, u.Division)
			assert.Equal(t, seed.DivBackend, u.Division.ID)
		}

		// Asking for another division is refused instead of silently widened.
		rec := api.Do(http.MethodGet, "/api/v1/members?division_id="+seed.DivFrontend, token, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Equal(t, "FORBIDDEN", testutil.ErrorCode(t, rec))
	})

	t.Run("member sees only themselves", func(t *testing.T) {
		token := api.Login(emailMemberBE1)
		body := listMembers(t, api, token, "")
		require.Equal(t, 1, body.Meta.Total)
		assert.Equal(t, seed.UserMemberBE1, body.Data[0].ID)

		// Filters cannot widen the scope.
		body = listMembers(t, api, token, "?role=lead")
		assert.Equal(t, 0, body.Meta.Total)
	})
}

func TestListMembersPaginationFilterSearchSort(t *testing.T) {
	api := testutil.NewAPI(t)
	token := api.Login(emailLead)

	t.Run("pagination", func(t *testing.T) {
		p1 := listMembers(t, api, token, "?limit=3&page=1&sort=email")
		assert.Len(t, p1.Data, 3)
		assert.Equal(t, 8, p1.Meta.Total)
		assert.Equal(t, 3, p1.Meta.TotalPages)

		p3 := listMembers(t, api, token, "?limit=3&page=3&sort=email")
		assert.Len(t, p3.Data, 2)

		beyond := listMembers(t, api, token, "?limit=3&page=4")
		assert.NotNil(t, beyond.Data, "empty page must be [] not null")
		assert.Empty(t, beyond.Data)
		assert.Equal(t, 8, beyond.Meta.Total)

		// Pages do not overlap.
		p2 := listMembers(t, api, token, "?limit=3&page=2&sort=email")
		seen := map[string]bool{}
		for _, u := range append(append(p1.Data, p2.Data...), p3.Data...) {
			assert.False(t, seen[u.ID], "duplicate across pages: %s", u.Email)
			seen[u.ID] = true
		}
		assert.Len(t, seen, 8)
	})

	t.Run("sort", func(t *testing.T) {
		asc := emails(listMembers(t, api, token, "?sort=email&order=asc&limit=100").Data)
		assert.True(t, sort.StringsAreSorted(asc), "%v", asc)

		desc := emails(listMembers(t, api, token, "?sort=email&order=desc&limit=100").Data)
		assert.True(t, sort.SliceIsSorted(desc, func(i, j int) bool { return desc[i] > desc[j] }), "%v", desc)
	})

	t.Run("filters", func(t *testing.T) {
		assert.Equal(t, 2, listMembers(t, api, token, "?role=division_head").Meta.Total)
		assert.Equal(t, 3, listMembers(t, api, token, "?division_id="+seed.DivBackend).Meta.Total)
		assert.Equal(t, 2, listMembers(t, api, token, "?role=member&division_id="+seed.DivBackend).Meta.Total)
	})

	t.Run("search name or email, case-insensitive", func(t *testing.T) {
		assert.Equal(t, 3, listMembers(t, api, token, "?search=BACKEND").Meta.Total)
		body := listMembers(t, api, token, "?search=andi")
		require.Equal(t, 1, body.Meta.Total)
		assert.Equal(t, "Andi Wijaya", body.Data[0].Name)
		assert.Equal(t, 1, listMembers(t, api, token, "?search=siti&role=member&division_id="+seed.DivBackend).Meta.Total)
	})

	t.Run("search treats wildcards and SQL literally", func(t *testing.T) {
		for _, q := range []string{"%25", "_", "%27%20OR%20%271%27%3D%271", "%5C"} {
			assert.Equal(t, 0, listMembers(t, api, token, "?search="+q).Meta.Total, "search=%s", q)
		}
	})
}

func TestListMembersRejectsInvalidQuery(t *testing.T) {
	api := testutil.NewAPI(t)
	token := api.Login(emailLead)

	for _, q := range []string{
		"limit=101", "limit=0", "page=0", "page=abc",
		"sort=password_hash", "sort=name%3BDROP%20TABLE%20users", "order=sideways",
		"role=admin", "division_id=not-a-uuid",
		"search=" + strings.Repeat("a", 101),
	} {
		rec := api.Do(http.MethodGet, "/api/v1/members?"+q, token, nil)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "query %q: %s", q, rec.Body.String())
		assert.Equal(t, "VALIDATION_ERROR", testutil.ErrorCode(t, rec))
	}
}

// ------------------------------------------------------------------- IDOR --

func TestGetMemberPreventsIDOR(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)
	headBE := api.Login(emailHeadBE)
	memberBE1 := api.Login(emailMemberBE1)

	cases := []struct {
		name   string
		token  string
		target string
		want   int
	}{
		{"member reads self", memberBE1, seed.UserMemberBE1, http.StatusOK},
		{"member reads same-division peer", memberBE1, seed.UserMemberBE2, http.StatusNotFound},
		{"member reads lead", memberBE1, seed.UserLead, http.StatusNotFound},
		{"head reads own division member", headBE, seed.UserMemberBE1, http.StatusOK},
		{"head reads other division member", headBE, seed.UserMemberFE, http.StatusNotFound},
		{"head reads other head", headBE, seed.UserHeadFrontend, http.StatusNotFound},
		{"head reads lead", headBE, seed.UserLead, http.StatusNotFound},
		{"lead reads anyone", lead, seed.UserMemberMobile, http.StatusOK},
		{"unknown id", lead, uuid.NewString(), http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := api.Do(http.MethodGet, memberPath(tc.target), tc.token, nil)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			if tc.want == http.StatusNotFound {
				// Out-of-scope and non-existent members look identical.
				assert.Equal(t, "NOT_FOUND", testutil.ErrorCode(t, rec))
				assert.NotContains(t, rec.Body.String(), "@")
			}
		})
	}

	rec := api.Do(http.MethodGet, memberPath("123"), lead, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ----------------------------------------------------------------- create --

func TestCreateMember(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)
	headBE := api.Login(emailHeadBE)

	t.Run("lead creates a division head who can log in", func(t *testing.T) {
		rec := api.Do(http.MethodPost, "/api/v1/members", lead, map[string]any{
			"name": "Budi Santoso", "email": "Head.Mobile@GDGoC-UNSRI.test", "password": "rahasia123",
			"role": "division_head", "division_id": seed.DivMobile, "phone": "+6281234567890",
		})
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var res struct{ Data userBody }
		testutil.DecodeJSON(t, rec, &res)
		assert.Equal(t, "head.mobile@gdgoc-unsri.test", res.Data.Email, "email is normalised")
		assert.Equal(t, "division_head", res.Data.Role)
		assert.Equal(t, seed.DivMobile, res.Data.Division.ID)
		assert.Equal(t, "/api/v1/members/"+res.Data.ID, rec.Header().Get("Location"))
		assert.NotContains(t, rec.Body.String(), "rahasia123")

		ok := login(api, "198.51.100.10", "head.mobile@gdgoc-unsri.test", "rahasia123")
		assert.Equal(t, http.StatusOK, ok.Code)
	})

	t.Run("account without password is Google-only", func(t *testing.T) {
		userFrom(t, api, http.MethodPost, "/api/v1/members", lead, map[string]any{
			"name": "Google User", "email": "google.only@gdgoc-unsri.test", "division_id": seed.DivUIUX,
		}, http.StatusCreated)
		rec := login(api, "198.51.100.11", "google.only@gdgoc-unsri.test", "anything123")
		assert.Equal(t, "INVALID_CREDENTIALS", testutil.ErrorCode(t, rec))
	})

	t.Run("division head creates member in own division by default", func(t *testing.T) {
		u := userFrom(t, api, http.MethodPost, "/api/v1/members", headBE, map[string]any{
			"name": "Rina Marlina", "email": "rina@gdgoc-unsri.test", "password": "rahasia123",
		}, http.StatusCreated)
		assert.Equal(t, "member", u.Role)
		assert.Equal(t, seed.DivBackend, u.Division.ID)
	})

	errCases := []struct {
		name  string
		token string
		body  map[string]any
		want  int
		code  string
	}{
		{"head adds to other division", headBE, map[string]any{"name": "X Y", "email": "x1@gdgoc-unsri.test", "division_id": seed.DivFrontend}, http.StatusForbidden, "FORBIDDEN"},
		{"head adds a division head", headBE, map[string]any{"name": "X Y", "email": "x2@gdgoc-unsri.test", "role": "division_head"}, http.StatusForbidden, "FORBIDDEN"},
		{"head adds a lead", headBE, map[string]any{"name": "X Y", "email": "x3@gdgoc-unsri.test", "role": "lead"}, http.StatusForbidden, "FORBIDDEN"},
		{"duplicate email, other case", lead, map[string]any{"name": "X Y", "email": "MEMBER.BACKEND1@gdgoc-unsri.test", "division_id": seed.DivBackend}, http.StatusConflict, "EMAIL_TAKEN"},
		{"member without division", lead, map[string]any{"name": "X Y", "email": "x4@gdgoc-unsri.test"}, http.StatusBadRequest, "DIVISION_REQUIRED"},
		{"unknown division", lead, map[string]any{"name": "X Y", "email": "x5@gdgoc-unsri.test", "division_id": uuid.NewString()}, http.StatusUnprocessableEntity, "DIVISION_NOT_FOUND"},
		{"missing fields", lead, map[string]any{}, http.StatusBadRequest, "VALIDATION_ERROR"},
		{"short password", lead, map[string]any{"name": "X Y", "email": "x6@gdgoc-unsri.test", "division_id": seed.DivBackend, "password": "short"}, http.StatusBadRequest, "VALIDATION_ERROR"},
		{"bad phone", lead, map[string]any{"name": "X Y", "email": "x7@gdgoc-unsri.test", "division_id": seed.DivBackend, "phone": "call me"}, http.StatusBadRequest, "VALIDATION_ERROR"},
		{"blank name", lead, map[string]any{"name": "   ", "email": "x8@gdgoc-unsri.test", "division_id": seed.DivBackend}, http.StatusBadRequest, "VALIDATION_ERROR"},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := api.Do(http.MethodPost, "/api/v1/members", tc.token, tc.body)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			assert.Equal(t, tc.code, testutil.ErrorCode(t, rec))
		})
	}

	t.Run("member cannot create members", func(t *testing.T) {
		rec := api.Do(http.MethodPost, "/api/v1/members", api.Login(emailMemberBE1), map[string]any{
			"name": "X Y", "email": "x9@gdgoc-unsri.test", "division_id": seed.DivBackend,
		})
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

// ----------------------------------------------------------------- update --

func TestUpdateMemberRBAC(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)
	headBE := api.Login(emailHeadBE)
	memberBE1 := api.Login(emailMemberBE1)

	// A second division head in Back-End, to check heads cannot edit each other.
	otherHead := userFrom(t, api, http.MethodPost, "/api/v1/members", lead, map[string]any{
		"name": "Kepala Kedua", "email": "head2.backend@gdgoc-unsri.test", "role": "division_head", "division_id": seed.DivBackend,
	}, http.StatusCreated)

	t.Run("head edits own division member, protected fields ignored", func(t *testing.T) {
		u := userFrom(t, api, http.MethodPatch, memberPath(seed.UserMemberBE1), headBE, map[string]any{
			"name": "Andi W.", "bio": "Updated by head",
			"role": "lead", "division_id": seed.DivFrontend, "deleted_at": "2020-01-01T00:00:00Z",
		}, http.StatusOK)
		assert.Equal(t, "Andi W.", u.Name)
		assert.Equal(t, "member", u.Role)
		assert.Equal(t, seed.DivBackend, u.Division.ID)
	})

	forbidden := []struct {
		name   string
		token  string
		target string
		want   int
	}{
		{"head edits other division member", headBE, seed.UserMemberFE, http.StatusNotFound},
		{"head edits lead", headBE, seed.UserLead, http.StatusNotFound},
		{"head edits other head in same division", headBE, otherHead.ID, http.StatusForbidden},
		{"head edits self via admin endpoint", headBE, seed.UserHeadBackend, http.StatusForbidden},
		{"member edits peer", memberBE1, seed.UserMemberBE2, http.StatusForbidden},
		{"member edits self via admin endpoint", memberBE1, seed.UserMemberBE1, http.StatusForbidden},
	}
	for _, tc := range forbidden {
		t.Run(tc.name, func(t *testing.T) {
			rec := api.Do(http.MethodPatch, memberPath(tc.target), tc.token, map[string]any{"name": "Hacked"})
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}

	t.Run("nothing was changed by refused requests", func(t *testing.T) {
		var n int
		require.NoError(t, api.Pool.QueryRow(context.Background(),
			`SELECT count(*) FROM users WHERE name = 'Hacked'`).Scan(&n))
		assert.Zero(t, n)
	})

	t.Run("lead edits email, duplicates rejected", func(t *testing.T) {
		u := userFrom(t, api, http.MethodPatch, memberPath(seed.UserHeadFrontend), lead,
			map[string]any{"email": "Nabila@GDGoC-UNSRI.test"}, http.StatusOK)
		assert.Equal(t, "nabila@gdgoc-unsri.test", u.Email)

		rec := api.Do(http.MethodPatch, memberPath(seed.UserHeadFrontend), lead, map[string]any{"email": emailLead})
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Equal(t, "EMAIL_TAKEN", testutil.ErrorCode(t, rec))
	})
}

func TestUpdateMeIgnoresProtectedFields(t *testing.T) {
	api := testutil.NewAPI(t)
	token := api.Login(emailMemberBE1)

	// Mass assignment attempt: everything except name/phone/bio must be ignored.
	u := userFrom(t, api, http.MethodPatch, "/api/v1/me", token, map[string]any{
		"name":          "Andi Baru",
		"bio":           "Halo",
		"role":          "lead",
		"division_id":   seed.DivFrontend,
		"deleted_at":    "2020-01-01T00:00:00Z",
		"email":         "evil@example.com",
		"id":            uuid.NewString(),
		"password_hash": "$2a$10$abc",
		"google_sub":    "attacker",
	}, http.StatusOK)
	assert.Equal(t, "Andi Baru", u.Name)
	assert.Equal(t, "member", u.Role)
	assert.Equal(t, seed.DivBackend, u.Division.ID)
	assert.Equal(t, emailMemberBE1, u.Email)

	// Verify straight from the database.
	var role, email, division string
	var deleted, googleSub *string
	require.NoError(t, api.Pool.QueryRow(context.Background(),
		`SELECT role::text, email, division_id::text, deleted_at::text, google_sub FROM users WHERE id = $1`,
		seed.UserMemberBE1).Scan(&role, &email, &division, &deleted, &googleSub))
	assert.Equal(t, "member", role)
	assert.Equal(t, emailMemberBE1, email)
	assert.Equal(t, seed.DivBackend, division)
	assert.Nil(t, deleted)
	assert.Nil(t, googleSub)

	// The member still cannot do Lead things with the same token.
	rec := api.Do(http.MethodPatch, memberPath(seed.UserMemberBE2)+"/role", token, map[string]any{"role": "lead"})
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// The old password still works (password_hash was not overwritten).
	assert.Equal(t, http.StatusOK, login(api, "198.51.100.20", emailMemberBE1, testutil.SeedPassword).Code)

	t.Run("empty string clears optional fields", func(t *testing.T) {
		u := userFrom(t, api, http.MethodPatch, "/api/v1/me", token, map[string]any{"phone": "", "bio": ""}, http.StatusOK)
		assert.Nil(t, u.Phone)
		assert.Nil(t, u.Bio)
		assert.Equal(t, "Andi Baru", u.Name, "omitted fields stay unchanged")
	})

	t.Run("validation", func(t *testing.T) {
		for _, body := range []map[string]any{
			{"name": "  "}, {"name": "A"}, {"phone": "12ab"}, {"bio": strings.Repeat("x", 501)},
		} {
			rec := api.Do(http.MethodPatch, "/api/v1/me", token, body)
			assert.Equal(t, http.StatusBadRequest, rec.Code, "%v: %s", body, rec.Body.String())
		}
	})
}

// ------------------------------------------------------------ role change --

func TestChangeRole(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)
	headBE := api.Login(emailHeadBE)
	memberBE1 := api.Login(emailMemberBE1)
	rolePath := memberPath(seed.UserMemberBE1) + "/role"

	t.Run("only lead may change roles", func(t *testing.T) {
		for _, token := range []string{headBE, memberBE1} {
			rec := api.Do(http.MethodPatch, rolePath, token, map[string]any{"role": "division_head"})
			assert.Equal(t, http.StatusForbidden, rec.Code)
		}
		assert.Zero(t, auditCount(t, api, "member.role_changed", seed.UserMemberBE1))
	})

	t.Run("lead promotes member and it is audited", func(t *testing.T) {
		u := userFrom(t, api, http.MethodPatch, rolePath, lead, map[string]any{"role": "division_head"}, http.StatusOK)
		assert.Equal(t, "division_head", u.Role)
		assert.Equal(t, seed.DivBackend, u.Division.ID, "division kept when omitted")

		var actor, ip string
		var oldVal, newVal []byte
		require.NoError(t, api.Pool.QueryRow(context.Background(), `
SELECT actor_id::text, host(ip), old_value, new_value FROM audit_logs
WHERE action = 'member.role_changed' AND entity = 'user' AND entity_id = $1`, seed.UserMemberBE1).
			Scan(&actor, &ip, &oldVal, &newVal))
		assert.Equal(t, seed.UserLead, actor)
		assert.NotEmpty(t, ip)
		assert.JSONEq(t, fmt.Sprintf(`{"role":"member","division_id":%q}`, seed.DivBackend), string(oldVal))
		assert.JSONEq(t, fmt.Sprintf(`{"role":"division_head","division_id":%q}`, seed.DivBackend), string(newVal))
	})

	t.Run("new role applies immediately to existing tokens", func(t *testing.T) {
		// memberBE1's token was issued while they were a member.
		body := listMembers(t, api, memberBE1, "")
		assert.Greater(t, body.Meta.Total, 1, "now sees the whole division")
	})

	t.Run("unchanged role writes no audit row", func(t *testing.T) {
		userFrom(t, api, http.MethodPatch, rolePath, lead, map[string]any{"role": "division_head"}, http.StatusOK)
		assert.Equal(t, 1, auditCount(t, api, "member.role_changed", seed.UserMemberBE1))
	})

	t.Run("move to another division", func(t *testing.T) {
		u := userFrom(t, api, http.MethodPatch, rolePath, lead,
			map[string]any{"role": "member", "division_id": seed.DivMobile}, http.StatusOK)
		assert.Equal(t, "member", u.Role)
		assert.Equal(t, seed.DivMobile, u.Division.ID)
		assert.Equal(t, 2, auditCount(t, api, "member.role_changed", seed.UserMemberBE1))
	})

	t.Run("lead cannot change own role", func(t *testing.T) {
		rec := api.Do(http.MethodPatch, memberPath(seed.UserLead)+"/role", lead,
			map[string]any{"role": "member", "division_id": seed.DivBackend})
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Equal(t, "CANNOT_CHANGE_OWN_ROLE", testutil.ErrorCode(t, rec))
	})

	t.Run("non-lead role needs a division", func(t *testing.T) {
		lead2 := userFrom(t, api, http.MethodPost, "/api/v1/members", lead, map[string]any{
			"name": "Lead Kedua", "email": "lead2@gdgoc-unsri.test", "role": "lead",
		}, http.StatusCreated)
		assert.Nil(t, lead2.Division)

		rec := api.Do(http.MethodPatch, memberPath(lead2.ID)+"/role", lead, map[string]any{"role": "member"})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "DIVISION_REQUIRED", testutil.ErrorCode(t, rec))

		rec = api.Do(http.MethodPatch, memberPath(lead2.ID)+"/role", lead,
			map[string]any{"role": "member", "division_id": uuid.NewString()})
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Equal(t, "DIVISION_NOT_FOUND", testutil.ErrorCode(t, rec))
		assert.Zero(t, auditCount(t, api, "member.role_changed", lead2.ID), "failed change is not audited")
	})

	t.Run("validation and unknown member", func(t *testing.T) {
		for _, body := range []map[string]any{{}, {"role": "admin"}, {"role": "member", "division_id": "x"}} {
			rec := api.Do(http.MethodPatch, rolePath, lead, body)
			assert.Equal(t, http.StatusBadRequest, rec.Code, "%v", body)
		}
		rec := api.Do(http.MethodPatch, memberPath(uuid.NewString())+"/role", lead, map[string]any{"role": "member"})
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

// Two Leads demoting each other at the same moment must never leave the
// organisation without a Lead: rows are locked and the actor's role is
// re-checked inside the transaction.
func TestConcurrentLeadsCannotDemoteEachOther(t *testing.T) {
	api := testutil.NewAPI(t)
	current := struct{ id, token string }{seed.UserLead, api.Login(emailLead)}

	for round := 0; round < 5; round++ {
		email := fmt.Sprintf("lead.round%d@gdgoc-unsri.test", round)
		other := userFrom(t, api, http.MethodPost, "/api/v1/members", current.token, map[string]any{
			"name": "Lead Ronde", "email": email, "role": "lead", "password": "rahasia123",
		}, http.StatusCreated)
		otherToken := api.LoginWith(email, "rahasia123")

		type result struct {
			code int
			id   string
		}
		results := make([]result, 2)
		attempts := []struct{ token, target, self string }{
			{current.token, other.ID, current.id},
			{otherToken, current.id, other.ID},
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i, a := range attempts {
			req := api.NewRequest(http.MethodPatch, memberPath(a.target)+"/role", a.token,
				map[string]any{"role": "member", "division_id": seed.DivBackend})
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				results[i] = result{code: api.Serve(req).Code, id: a.self}
			}()
		}
		close(start)
		wg.Wait()

		var winners []result
		for _, r := range results {
			if r.code == http.StatusOK {
				winners = append(winners, r)
			} else {
				assert.Equal(t, http.StatusForbidden, r.code)
			}
		}
		require.Len(t, winners, 1, "round %d: %+v", round, results)

		var leads int
		require.NoError(t, api.Pool.QueryRow(context.Background(),
			`SELECT count(*) FROM users WHERE role = 'lead' AND deleted_at IS NULL`).Scan(&leads))
		require.Equal(t, 1, leads, "round %d", round)

		if winners[0].id == other.ID {
			current.id, current.token = other.ID, otherToken
		}
	}
}

// ----------------------------------------------------------------- delete --

func TestDeleteMember(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)
	headBE := api.Login(emailHeadBE)
	memberBE1 := api.Login(emailMemberBE1)
	memberBE2 := api.Login(emailMemberBE2)

	cases := []struct {
		name   string
		token  string
		target string
		want   int
		code   string
	}{
		{"member deletes peer", memberBE2, seed.UserMemberBE1, http.StatusForbidden, "FORBIDDEN"},
		{"head deletes other division member", headBE, seed.UserMemberFE, http.StatusNotFound, "NOT_FOUND"},
		{"head deletes lead", headBE, seed.UserLead, http.StatusNotFound, "NOT_FOUND"},
		{"head deletes self", headBE, seed.UserHeadBackend, http.StatusForbidden, "CANNOT_DELETE_SELF"},
		{"lead deletes self", lead, seed.UserLead, http.StatusForbidden, "CANNOT_DELETE_SELF"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := api.Do(http.MethodDelete, memberPath(tc.target), tc.token, nil)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			assert.Equal(t, tc.code, testutil.ErrorCode(t, rec))
		})
	}

	t.Run("head soft-deletes own division member", func(t *testing.T) {
		rec := api.Do(http.MethodDelete, memberPath(seed.UserMemberBE1), headBE, nil)
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		assert.Empty(t, rec.Body.String())

		// Row still exists (soft delete) and the deletion is audited.
		var deleted bool
		require.NoError(t, api.Pool.QueryRow(context.Background(),
			`SELECT deleted_at IS NOT NULL FROM users WHERE id = $1`, seed.UserMemberBE1).Scan(&deleted))
		assert.True(t, deleted)

		var actor string
		var oldVal []byte
		require.NoError(t, api.Pool.QueryRow(context.Background(),
			`SELECT actor_id::text, old_value FROM audit_logs WHERE action = 'member.deleted' AND entity_id = $1`,
			seed.UserMemberBE1).Scan(&actor, &oldVal))
		assert.Equal(t, seed.UserHeadBackend, actor)
		var snap map[string]any
		require.NoError(t, json.Unmarshal(oldVal, &snap))
		assert.Equal(t, emailMemberBE1, snap["email"])
	})

	t.Run("deleted member is gone everywhere", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, api.Do(http.MethodGet, "/api/v1/me", memberBE1, nil).Code, "token revoked")
		assert.Equal(t, http.StatusUnauthorized, login(api, "198.51.100.30", emailMemberBE1, testutil.SeedPassword).Code)
		assert.Equal(t, http.StatusNotFound, api.Do(http.MethodGet, memberPath(seed.UserMemberBE1), lead, nil).Code)
		assert.Equal(t, 7, listMembers(t, api, lead, "").Meta.Total)
		assert.Equal(t, http.StatusNotFound, api.Do(http.MethodDelete, memberPath(seed.UserMemberBE1), headBE, nil).Code)
		assert.Equal(t, 1, auditCount(t, api, "member.deleted", seed.UserMemberBE1))
	})

	t.Run("lead deletes a division head", func(t *testing.T) {
		rec := api.Do(http.MethodDelete, memberPath(seed.UserHeadFrontend), lead, nil)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("audit log is append-only", func(t *testing.T) {
		_, err := api.Pool.Exec(context.Background(), `UPDATE audit_logs SET action = 'tampered'`)
		assert.ErrorContains(t, err, "append-only")
		_, err = api.Pool.Exec(context.Background(), `DELETE FROM audit_logs`)
		assert.ErrorContains(t, err, "append-only")
	})
}

func TestListDivisions(t *testing.T) {
	api := testutil.NewAPI(t)
	rec := api.Do(http.MethodGet, "/api/v1/divisions", api.Login(emailMemberFE), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var res struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	testutil.DecodeJSON(t, rec, &res)
	assert.Len(t, res.Data, 4)

	assert.Equal(t, http.StatusUnauthorized, api.Do(http.MethodGet, "/api/v1/divisions", "", nil).Code)
}
