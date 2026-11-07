package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mauinGit/gdgoc-core-team-api/internal/testutil"
	"github.com/mauinGit/gdgoc-core-team-api/seed"
)

type attendanceBody struct {
	ID          string `json:"id"`
	EventID     string `json:"event_id"`
	UserID      string `json:"user_id"`
	CheckedInAt string `json:"checked_in_at"`
}

func checkInPath(eventID string) string { return eventPath(eventID) + "/check-in" }

func checkIn(t *testing.T, api *testutil.API, token, eventID string, want int) attendanceBody {
	t.Helper()
	rec := api.Do(http.MethodPost, checkInPath(eventID), token, nil)
	require.Equal(t, want, rec.Code, rec.Body.String())
	var res struct{ Data attendanceBody }
	testutil.DecodeJSON(t, rec, &res)
	return res.Data
}

func attendanceRows(t *testing.T, api *testutil.API, eventID, userID string) int {
	t.Helper()
	var n int
	require.NoError(t, api.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM attendances WHERE event_id = $1 AND user_id = $2`, eventID, userID).Scan(&n))
	return n
}

func TestCheckInIsIdempotent(t *testing.T) {
	api := testutil.NewAPI(t)
	be1 := api.Login(emailMemberBE1) // registered for the ongoing bootcamp

	first := checkIn(t, api, be1, seed.EventBootcampOngoing, http.StatusCreated)
	assert.Equal(t, seed.UserMemberBE1, first.UserID)

	second := checkIn(t, api, be1, seed.EventBootcampOngoing, http.StatusOK)
	assert.Equal(t, first, second, "repeat check-in returns the existing row")
	assert.Equal(t, 1, attendanceRows(t, api, seed.EventBootcampOngoing, seed.UserMemberBE1))

	// Once checked in, the registration can no longer be cancelled.
	rec := api.Do(http.MethodDelete, eventPath(seed.EventBootcampOngoing)+"/register", be1, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "ALREADY_CHECKED_IN", testutil.ErrorCode(t, rec))
}

func TestConcurrentCheckInCreatesOneRow(t *testing.T) {
	api := testutil.NewAPI(t)
	be2 := api.Login(emailMemberBE2) // registered for the ongoing study jam

	tokens := make([]string, 20)
	for i := range tokens {
		tokens[i] = be2
	}
	codes, bodies := postConcurrently(api, checkInPath(seed.EventStudyJamOngoing), tokens)

	assert.Equal(t, 1, count(codes, http.StatusCreated), "exactly one request creates the row")
	assert.Equal(t, 19, count(codes, http.StatusOK), "the others get the existing row")

	ids := map[string]bool{}
	for _, b := range bodies {
		var res struct{ Data attendanceBody }
		require.NoError(t, json.Unmarshal([]byte(b), &res))
		ids[res.Data.ID] = true
	}
	assert.Len(t, ids, 1, "every response carries the same attendance")
	assert.Equal(t, 1, attendanceRows(t, api, seed.EventStudyJamOngoing, seed.UserMemberBE2))
}

func TestCheckInWindowAndEligibility(t *testing.T) {
	api := testutil.NewAPI(t)
	ctx := context.Background()
	be1 := api.Login(emailMemberBE1)
	be2 := api.Login(emailMemberBE2)
	headFE := api.Login(emailHeadFE)

	// member.backend2 holds a registration (without check-in) for the finished kickoff.
	_, err := api.Pool.Exec(ctx,
		`INSERT INTO event_registrations (event_id, user_id) VALUES ($1, $2)`, seed.EventKickoffPast, seed.UserMemberBE2)
	require.NoError(t, err)

	// member.backend1 registers for the upcoming tech talk.
	require.Equal(t, http.StatusCreated, api.Do(http.MethodPost, eventPath(seed.EventTechTalk)+"/register", be1, nil).Code)

	// member.backend2 registers for the bootcamp and cancels.
	require.Equal(t, http.StatusCreated, api.Do(http.MethodPost, eventPath(seed.EventBootcampOngoing)+"/register", be2, nil).Code)
	require.Equal(t, http.StatusNoContent, api.Do(http.MethodDelete, eventPath(seed.EventBootcampOngoing)+"/register", be2, nil).Code)

	cases := []struct {
		name   string
		token  string
		event  string
		userID string
		want   int
		code   string
	}{
		{"before start", be1, seed.EventTechTalk, seed.UserMemberBE1, http.StatusConflict, "CHECK_IN_NOT_OPEN"},
		{"after end", be2, seed.EventKickoffPast, seed.UserMemberBE2, http.StatusConflict, "EVENT_ENDED"},
		{"not registered", headFE, seed.EventBootcampOngoing, seed.UserHeadFrontend, http.StatusForbidden, "NOT_REGISTERED"},
		{"registration cancelled", be2, seed.EventBootcampOngoing, seed.UserMemberBE2, http.StatusForbidden, "NOT_REGISTERED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := api.Do(http.MethodPost, checkInPath(tc.event), tc.token, nil)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			assert.Equal(t, tc.code, testutil.ErrorCode(t, rec))
			assert.Zero(t, attendanceRows(t, api, tc.event, tc.userID), "refused check-in writes nothing")
		})
	}

	t.Run("unknown event", func(t *testing.T) {
		rec := api.Do(http.MethodPost, checkInPath(uuid.NewString()), be1, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("retry after the event ended returns the existing check-in", func(t *testing.T) {
		// member.backend1 checked in to the kickoff while it was running (seed).
		a := checkIn(t, api, be1, seed.EventKickoffPast, http.StatusOK)
		assert.Equal(t, seed.UserMemberBE1, a.UserID)
	})
}

func TestAttendanceListIsScoped(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)
	headBE := api.Login(emailHeadBE)
	headFE := api.Login(emailHeadFE)
	be1 := api.Login(emailMemberBE1)
	be2 := api.Login(emailMemberBE2)
	fe := api.Login(emailMemberFE)

	bootcamp := seed.EventBootcampOngoing
	require.Equal(t, http.StatusCreated, api.Do(http.MethodPost, eventPath(bootcamp)+"/register", fe, nil).Code)
	checkIn(t, api, fe, bootcamp, http.StatusCreated)
	checkIn(t, api, be1, bootcamp, http.StatusCreated)

	attendees := func(token string) []string {
		t.Helper()
		rec := api.Do(http.MethodGet, eventPath(bootcamp)+"/attendances", token, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var b struct {
			Data []struct {
				User struct {
					Email string `json:"email"`
				} `json:"user"`
				CheckedInAt string `json:"checked_in_at"`
			} `json:"data"`
		}
		testutil.DecodeJSON(t, rec, &b)
		out := []string{}
		for _, a := range b.Data {
			assert.NotEmpty(t, a.CheckedInAt)
			out = append(out, a.User.Email)
		}
		return out
	}

	assert.ElementsMatch(t, []string{emailMemberBE1, emailMemberFE}, attendees(lead))
	assert.ElementsMatch(t, []string{emailMemberBE1, emailMemberFE}, attendees(headBE), "own division event")
	assert.ElementsMatch(t, []string{emailMemberFE}, attendees(headFE), "other division event: own members only")
	assert.ElementsMatch(t, []string{emailMemberBE1}, attendees(be1), "member: own data only")
	assert.Empty(t, attendees(be2))
}

func TestAuditLogs(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)

	// One entry of each audited action, oldest first.
	require.Equal(t, http.StatusOK, api.Do(http.MethodPatch, memberPath(seed.UserMemberBE1)+"/role", lead,
		map[string]any{"role": "division_head"}).Code)
	require.Equal(t, http.StatusNoContent, api.Do(http.MethodDelete, memberPath(seed.UserMemberBE2), lead, nil).Code)
	require.Equal(t, http.StatusOK, api.Do(http.MethodPatch, eventPath(seed.EventBootcampOngoing), lead,
		map[string]any{"quota": 40}).Code)

	type logBody struct {
		ID    int64 `json:"id"`
		Actor *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"actor"`
		Action   string          `json:"action"`
		Entity   string          `json:"entity"`
		EntityID string          `json:"entity_id"`
		OldValue json.RawMessage `json:"old_value"`
		NewValue json.RawMessage `json:"new_value"`
		IP       *string         `json:"ip"`
	}
	list := func(token, query string) (int, []logBody, int) {
		t.Helper()
		rec := api.Do(http.MethodGet, "/api/v1/audit-logs"+query, token, nil)
		if rec.Code != http.StatusOK {
			return rec.Code, nil, 0
		}
		var b struct {
			Data []logBody `json:"data"`
			Meta struct {
				Total int `json:"total"`
			} `json:"meta"`
		}
		testutil.DecodeJSON(t, rec, &b)
		return rec.Code, b.Data, b.Meta.Total
	}

	t.Run("lead sees every entry, newest first", func(t *testing.T) {
		code, logs, total := list(lead, "")
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, 3, total)
		assert.Equal(t, []string{"event.quota_changed", "member.deleted", "member.role_changed"},
			[]string{logs[0].Action, logs[1].Action, logs[2].Action})

		q := logs[0]
		assert.Equal(t, "event", q.Entity)
		assert.Equal(t, seed.EventBootcampOngoing, q.EntityID)
		require.NotNil(t, q.Actor)
		assert.Equal(t, seed.UserLead, q.Actor.ID)
		assert.Equal(t, "Rizky Ramadhan", q.Actor.Name)
		assert.JSONEq(t, `{"quota":30,"registered_count":1}`, string(q.OldValue))
		assert.JSONEq(t, `{"quota":40,"registered_count":1}`, string(q.NewValue))
		assert.NotNil(t, q.IP)
	})

	t.Run("filters and pagination", func(t *testing.T) {
		_, logs, total := list(lead, "?action=member.deleted")
		require.Equal(t, 1, total)
		assert.Equal(t, seed.UserMemberBE2, logs[0].EntityID)

		_, _, total = list(lead, "?entity=event")
		assert.Equal(t, 1, total)
		_, _, total = list(lead, "?entity_id="+seed.UserMemberBE1)
		assert.Equal(t, 1, total)
		_, _, total = list(lead, "?actor_id="+seed.UserLead)
		assert.Equal(t, 3, total)

		_, logs, total = list(lead, "?limit=1&page=2")
		assert.Equal(t, 3, total)
		require.Len(t, logs, 1)
		assert.Equal(t, "member.deleted", logs[0].Action)
	})

	t.Run("lead only", func(t *testing.T) {
		for _, email := range []string{emailHeadBE, emailMemberFE} {
			code, _, _ := list(api.Login(email), "")
			assert.Equal(t, http.StatusForbidden, code, email)
		}
		// member.backend1 was promoted to division head above; still not a Lead.
		code, _, _ := list(api.Login(emailMemberBE1), "")
		assert.Equal(t, http.StatusForbidden, code)
	})

	t.Run("validation", func(t *testing.T) {
		for _, q := range []string{"?action=member.created", "?entity=division", "?limit=101", "?entity_id=x", "?actor_id=1"} {
			code, _, _ := list(lead, q)
			assert.Equal(t, http.StatusBadRequest, code, q)
		}
	})
}
