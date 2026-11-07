package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mauinGit/gdgoc-core-team-api/internal/testutil"
	"github.com/mauinGit/gdgoc-core-team-api/seed"
)

type eventBody struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Division *struct {
		ID string `json:"id"`
	} `json:"division"`
	Status          string `json:"status"`
	Quota           int    `json:"quota"`
	RegisteredCount int    `json:"registered_count"`
	RemainingQuota  int    `json:"remaining_quota"`
}

func eventPath(id string) string { return "/api/v1/events/" + id }

func rfc3339(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }

func newEventBody(title string, quota int) map[string]any {
	return map[string]any{
		"title": title, "location": "Lab Fasilkom", "quota": quota,
		"start_at": rfc3339(-time.Hour), "end_at": rfc3339(24 * time.Hour),
	}
}

func createEvent(t *testing.T, api *testutil.API, token string, body map[string]any) eventBody {
	t.Helper()
	rec := api.Do(http.MethodPost, "/api/v1/events", token, body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var res struct{ Data eventBody }
	testutil.DecodeJSON(t, rec, &res)
	return res.Data
}

func getEvent(t *testing.T, api *testutil.API, token, id string) eventBody {
	t.Helper()
	rec := api.Do(http.MethodGet, eventPath(id), token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var res struct{ Data eventBody }
	testutil.DecodeJSON(t, rec, &res)
	return res.Data
}

// registrationCounts returns events.registered_count and the number of active
// registration rows; the two must always agree.
func registrationCounts(t *testing.T, api *testutil.API, eventID string) (counter, rows int) {
	t.Helper()
	require.NoError(t, api.Pool.QueryRow(context.Background(), `
SELECT e.registered_count,
       (SELECT count(*) FROM event_registrations r WHERE r.event_id = e.id AND r.status = 'registered')
FROM events e WHERE e.id = $1`, eventID).Scan(&counter, &rows))
	return counter, rows
}

// createMembers inserts n members directly (sharing the seed password hash)
// and returns their access tokens.
func createMembers(t *testing.T, api *testutil.API, prefix string, n int) []string {
	t.Helper()
	_, err := api.Pool.Exec(context.Background(), `
INSERT INTO users (name, email, password_hash, role, division_id)
SELECT 'Peserta ' || g, $1 || g || '@gdgoc-unsri.test',
       (SELECT password_hash FROM users WHERE id = $2), 'member', $3
FROM generate_series(1, $4) AS g`, prefix, seed.UserLead, seed.DivBackend, n)
	require.NoError(t, err)

	tokens := make([]string, n)
	for i := range tokens {
		tokens[i] = api.Login(fmt.Sprintf("%s%d@gdgoc-unsri.test", prefix, i+1))
	}
	return tokens
}

// postConcurrently sends one POST per token to path at the same instant and
// returns the status codes and response bodies.
func postConcurrently(api *testutil.API, path string, tokens []string) (codes []int, bodies []string) {
	api.WarmPool(len(tokens))
	reqs := make([]*http.Request, len(tokens))
	for i, tok := range tokens {
		reqs[i] = api.NewRequest(http.MethodPost, path, tok, nil)
	}
	codes = make([]int, len(tokens))
	bodies = make([]string, len(tokens))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, req := range reqs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rec := api.Serve(req)
			codes[i], bodies[i] = rec.Code, rec.Body.String()
		}()
	}
	close(start)
	wg.Wait()
	return codes, bodies
}

// fireConcurrently sends one register request per token at the same instant
// and returns the status codes and error codes.
func fireConcurrently(api *testutil.API, eventID string, tokens []string) (codes []int, errCodes map[string]int) {
	codes, bodies := postConcurrently(api, eventPath(eventID)+"/register", tokens)
	errCodes = map[string]int{}
	for i, code := range codes {
		if code != http.StatusCreated {
			var b struct {
				Error struct{ Code string } `json:"error"`
			}
			_ = json.Unmarshal([]byte(bodies[i]), &b)
			errCodes[b.Error.Code]++
		}
	}
	return codes, errCodes
}

func count(codes []int, want int) int {
	n := 0
	for _, c := range codes {
		if c == want {
			n++
		}
	}
	return n
}

// ------------------------------------------------------------------ quota --

// The core guarantee: 50 members race for 10 slots, exactly 10 win.
func TestRegisterQuotaIsNeverExceededUnderConcurrency(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)
	tokens := createMembers(t, api, "racer", 50)

	for round := 1; round <= 3; round++ {
		ev := createEvent(t, api, lead, newEventBody(fmt.Sprintf("Race %d", round), 10))

		codes, errCodes := fireConcurrently(api, ev.ID, tokens)

		assert.Equal(t, 10, count(codes, http.StatusCreated), "round %d: exactly quota registrations succeed", round)
		assert.Equal(t, 40, count(codes, http.StatusConflict), "round %d", round)
		assert.Equal(t, map[string]int{"QUOTA_FULL": 40}, errCodes, "round %d", round)

		counter, rows := registrationCounts(t, api, ev.ID)
		assert.Equal(t, 10, counter, "round %d: registered_count", round)
		assert.Equal(t, 10, rows, "round %d: registration rows", round)

		got := getEvent(t, api, lead, ev.ID)
		assert.Equal(t, 0, got.RemainingQuota)
	}
}

func TestDoubleRegistration(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)
	member := api.Login(emailMemberBE2)

	t.Run("sequential", func(t *testing.T) {
		ev := createEvent(t, api, lead, newEventBody("Double", 5))
		rec := api.Do(http.MethodPost, eventPath(ev.ID)+"/register", member, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		rec = api.Do(http.MethodPost, eventPath(ev.ID)+"/register", member, nil)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Equal(t, "ALREADY_REGISTERED", testutil.ErrorCode(t, rec))

		counter, rows := registrationCounts(t, api, ev.ID)
		assert.Equal(t, 1, counter, "failed duplicate must not consume a slot")
		assert.Equal(t, 1, rows)
	})

	t.Run("concurrent, same member", func(t *testing.T) {
		ev := createEvent(t, api, lead, newEventBody("Double concurrent", 5))
		tokens := make([]string, 20)
		for i := range tokens {
			tokens[i] = member
		}
		codes, errCodes := fireConcurrently(api, ev.ID, tokens)

		assert.Equal(t, 1, count(codes, http.StatusCreated))
		assert.Equal(t, map[string]int{"ALREADY_REGISTERED": 19}, errCodes)
		counter, rows := registrationCounts(t, api, ev.ID)
		assert.Equal(t, 1, counter, "rolled-back duplicates give their slot back")
		assert.Equal(t, 1, rows)
	})
}

func TestCancelRegistrationReturnsSlot(t *testing.T) {
	api := testutil.NewAPI(t)
	be1 := api.Login(emailMemberBE1)
	be2 := api.Login(emailMemberBE2)
	hackathon := seed.EventHackathonSmall // quota 2, member.frontend already registered

	require.Equal(t, http.StatusCreated, api.Do(http.MethodPost, eventPath(hackathon)+"/register", be2, nil).Code)
	rec := api.Do(http.MethodPost, eventPath(hackathon)+"/register", be1, nil)
	assert.Equal(t, "QUOTA_FULL", testutil.ErrorCode(t, rec))

	rec = api.Do(http.MethodDelete, eventPath(hackathon)+"/register", be2, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	counter, rows := registrationCounts(t, api, hackathon)
	assert.Equal(t, 1, counter)
	assert.Equal(t, 1, rows)

	// The freed slot can be taken by someone else.
	assert.Equal(t, http.StatusCreated, api.Do(http.MethodPost, eventPath(hackathon)+"/register", be1, nil).Code)

	// Cancelling twice, or without a registration, is a 404.
	rec = api.Do(http.MethodDelete, eventPath(hackathon)+"/register", be2, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Re-registering after cancelling reuses the row (event is full again now).
	rec = api.Do(http.MethodPost, eventPath(hackathon)+"/register", be2, nil)
	assert.Equal(t, "QUOTA_FULL", testutil.ErrorCode(t, rec))
	require.Equal(t, http.StatusNoContent, api.Do(http.MethodDelete, eventPath(hackathon)+"/register", be1, nil).Code)
	rec = api.Do(http.MethodPost, eventPath(hackathon)+"/register", be2, nil)
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	counter, rows = registrationCounts(t, api, hackathon)
	assert.Equal(t, 2, counter)
	assert.Equal(t, 2, rows)
}

func TestRegistrationRules(t *testing.T) {
	api := testutil.NewAPI(t)
	be1 := api.Login(emailMemberBE1)
	uiux := api.Login("member.uiux@" + seed.EmailDomain)

	cases := []struct {
		name   string
		method string
		event  string
		token  string
		want   int
		code   string
	}{
		{"register for finished event", http.MethodPost, seed.EventKickoffPast, be1, http.StatusConflict, "EVENT_ENDED"},
		{"cancel on finished event", http.MethodDelete, seed.EventKickoffPast, be1, http.StatusConflict, "EVENT_ENDED"},
		{"register for full event", http.MethodPost, seed.EventDesignSprintFull, be1, http.StatusConflict, "QUOTA_FULL"},
		{"already registered on full event", http.MethodPost, seed.EventDesignSprintFull, uiux, http.StatusConflict, "ALREADY_REGISTERED"},
		{"unknown event", http.MethodPost, uuid.NewString(), be1, http.StatusNotFound, "NOT_FOUND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := api.Do(tc.method, eventPath(tc.event)+"/register", tc.token, nil)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			assert.Equal(t, tc.code, testutil.ErrorCode(t, rec))
		})
	}

	t.Run("cannot cancel after check-in", func(t *testing.T) {
		_, err := api.Pool.Exec(context.Background(),
			`INSERT INTO attendances (event_id, user_id) VALUES ($1, $2)`, seed.EventBootcampOngoing, seed.UserMemberBE1)
		require.NoError(t, err)
		rec := api.Do(http.MethodDelete, eventPath(seed.EventBootcampOngoing)+"/register", be1, nil)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Equal(t, "ALREADY_CHECKED_IN", testutil.ErrorCode(t, rec))
	})

	t.Run("deleted event is gone", func(t *testing.T) {
		lead := api.Login(emailLead)
		ev := createEvent(t, api, lead, newEventBody("Temporary", 5))
		require.Equal(t, http.StatusNoContent, api.Do(http.MethodDelete, eventPath(ev.ID), lead, nil).Code)
		assert.Equal(t, http.StatusNotFound, api.Do(http.MethodGet, eventPath(ev.ID), be1, nil).Code)
		assert.Equal(t, http.StatusNotFound, api.Do(http.MethodPost, eventPath(ev.ID)+"/register", be1, nil).Code)
	})
}

func TestDeletingMemberReleasesTheirSlots(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)

	// member.frontend holds a slot on the upcoming hackathon and attended the
	// past kickoff.
	require.Equal(t, http.StatusNoContent, api.Do(http.MethodDelete, memberPath(seed.UserMemberFE), lead, nil).Code)

	counter, rows := registrationCounts(t, api, seed.EventHackathonSmall)
	assert.Equal(t, 0, counter, "upcoming slot released")
	assert.Equal(t, 0, rows)

	_, rows = registrationCounts(t, api, seed.EventKickoffPast)
	assert.Equal(t, 2, rows, "history of finished events is kept")

	var newVal string
	require.NoError(t, api.Pool.QueryRow(context.Background(),
		`SELECT new_value::text FROM audit_logs WHERE action = 'member.deleted' AND entity_id = $1`,
		seed.UserMemberFE).Scan(&newVal))
	assert.JSONEq(t, `{"cancelled_registrations":1}`, newVal)
}

// ------------------------------------------------------------ event CRUD --

func TestEventManagementRBAC(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)
	headBE := api.Login(emailHeadBE)
	member := api.Login(emailMemberBE1)

	t.Run("create", func(t *testing.T) {
		general := createEvent(t, api, lead, newEventBody("General by lead", 10))
		assert.Nil(t, general.Division, "lead may create general events")

		own := createEvent(t, api, headBE, newEventBody("BE by head", 10))
		require.NotNil(t, own.Division)
		assert.Equal(t, seed.DivBackend, own.Division.ID, "division defaults to the head's own")
		assert.Equal(t, "ongoing", own.Status)

		other := newEventBody("FE by BE head", 10)
		other["division_id"] = seed.DivFrontend
		rec := api.Do(http.MethodPost, "/api/v1/events", headBE, other)
		assert.Equal(t, http.StatusForbidden, rec.Code)

		rec = api.Do(http.MethodPost, "/api/v1/events", member, newEventBody("By member", 10))
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("update and delete", func(t *testing.T) {
		cases := []struct {
			name  string
			token string
			event string
			want  int
		}{
			{"member updates", member, seed.EventBootcampOngoing, http.StatusForbidden},
			{"head updates other division event", headBE, seed.EventHackathonSmall, http.StatusForbidden},
			{"head updates general event", headBE, seed.EventStudyJamOngoing, http.StatusForbidden},
			{"head updates own division event", headBE, seed.EventBootcampOngoing, http.StatusOK},
			{"lead updates any event", lead, seed.EventHackathonSmall, http.StatusOK},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				rec := api.Do(http.MethodPatch, eventPath(tc.event), tc.token, map[string]any{"title": "Judul Baru " + tc.name})
				assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			})
		}

		assert.Equal(t, http.StatusForbidden, api.Do(http.MethodDelete, eventPath(seed.EventHackathonSmall), headBE, nil).Code)
		assert.Equal(t, http.StatusForbidden, api.Do(http.MethodDelete, eventPath(seed.EventBootcampOngoing), member, nil).Code)
		assert.Equal(t, http.StatusNoContent, api.Do(http.MethodDelete, eventPath(seed.EventBootcampOngoing), headBE, nil).Code)
	})

	t.Run("protected fields are ignored", func(t *testing.T) {
		rec := api.Do(http.MethodPatch, eventPath(seed.EventTechTalk), lead, map[string]any{
			"registered_count": 0, "division_id": seed.DivMobile, "created_by": seed.UserMemberBE1, "location": "Aula Baru",
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		got := getEvent(t, api, lead, seed.EventTechTalk)
		assert.Nil(t, got.Division)
	})

	t.Run("validation", func(t *testing.T) {
		bad := []map[string]any{
			{"title": "X", "location": "L", "quota": 10, "start_at": rfc3339(time.Hour), "end_at": rfc3339(2 * time.Hour)},
			{"title": "Valid", "location": "L", "quota": 0, "start_at": rfc3339(time.Hour), "end_at": rfc3339(2 * time.Hour)},
			{"title": "Valid", "location": "L", "quota": 10, "start_at": rfc3339(2 * time.Hour), "end_at": rfc3339(time.Hour)},
			{"title": "Valid", "location": "L", "quota": 10, "start_at": "besok pagi", "end_at": rfc3339(time.Hour)},
			{"title": "Valid", "location": "   ", "quota": 10, "start_at": rfc3339(time.Hour), "end_at": rfc3339(2 * time.Hour)},
		}
		for _, body := range bad {
			rec := api.Do(http.MethodPost, "/api/v1/events", lead, body)
			assert.Equal(t, http.StatusBadRequest, rec.Code, "%v: %s", body, rec.Body.String())
		}

		// end_at before the existing start_at.
		rec := api.Do(http.MethodPatch, eventPath(seed.EventTechTalk), lead, map[string]any{"end_at": rfc3339(-48 * time.Hour)})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "INVALID_TIME_RANGE", testutil.ErrorCode(t, rec))
	})
}

func TestEventQuotaChangeIsAuditedAndValidated(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)
	ev := createEvent(t, api, lead, newEventBody("Quota change", 5))
	for _, email := range []string{emailMemberBE1, emailMemberBE2} {
		require.Equal(t, http.StatusCreated, api.Do(http.MethodPost, eventPath(ev.ID)+"/register", api.Login(email), nil).Code)
	}

	rec := api.Do(http.MethodPatch, eventPath(ev.ID), lead, map[string]any{"quota": 1})
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "QUOTA_BELOW_REGISTERED", testutil.ErrorCode(t, rec))
	assert.Zero(t, auditCount(t, api, "event.quota_changed", ev.ID))

	rec = api.Do(http.MethodPatch, eventPath(ev.ID), lead, map[string]any{"title": "Only title"})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Zero(t, auditCount(t, api, "event.quota_changed", ev.ID), "no audit without a quota change")

	rec = api.Do(http.MethodPatch, eventPath(ev.ID), lead, map[string]any{"quota": 2})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var oldVal, newVal, actor string
	require.NoError(t, api.Pool.QueryRow(context.Background(), `
SELECT old_value::text, new_value::text, actor_id::text FROM audit_logs
WHERE action = 'event.quota_changed' AND entity = 'event' AND entity_id = $1`, ev.ID).Scan(&oldVal, &newVal, &actor))
	assert.JSONEq(t, `{"quota":5,"registered_count":2}`, oldVal)
	assert.JSONEq(t, `{"quota":2,"registered_count":2}`, newVal)
	assert.Equal(t, seed.UserLead, actor)

	got := getEvent(t, api, lead, ev.ID)
	assert.Equal(t, 2, got.Quota)
	assert.Equal(t, 0, got.RemainingQuota)
}

func TestListEvents(t *testing.T) {
	api := testutil.NewAPI(t)
	token := api.Login(emailMemberFE)

	list := func(query string) listBody {
		t.Helper()
		rec := api.Do(http.MethodGet, "/api/v1/events"+query, token, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var b listBody
		testutil.DecodeJSON(t, rec, &b)
		return b
	}
	assert.Equal(t, 6, list("").Meta.Total)
	assert.Equal(t, 2, list("?status=ongoing").Meta.Total)
	assert.Equal(t, 3, list("?status=upcoming").Meta.Total)
	assert.Equal(t, 1, list("?status=finished").Meta.Total)
	assert.Equal(t, 1, list("?division_id="+seed.DivFrontend).Meta.Total)
	assert.Equal(t, 1, list("?search=hackathon").Meta.Total)
	assert.Equal(t, 0, list("?search=%25").Meta.Total)
	p := list("?limit=4&page=2")
	assert.Equal(t, 2, p.Meta.TotalPages)
	assert.Len(t, p.Data, 2)

	for _, q := range []string{"?sort=quota", "?status=soon", "?limit=500", "?division_id=x"} {
		assert.Equal(t, http.StatusBadRequest, api.Do(http.MethodGet, "/api/v1/events"+q, token, nil).Code, q)
	}
}

// ----------------------------------------------------------- participants --

func TestParticipantsAreScopedByRole(t *testing.T) {
	api := testutil.NewAPI(t)
	lead := api.Login(emailLead)
	headBE := api.Login(emailHeadBE)
	headFE := api.Login(emailHeadFE)
	be2 := api.Login(emailMemberBE2)

	// Bootcamp (Back-End event) gets one Front-End participant besides member.backend1.
	require.Equal(t, http.StatusCreated,
		api.Do(http.MethodPost, eventPath(seed.EventBootcampOngoing)+"/register", api.Login(emailMemberFE), nil).Code)

	participants := func(token, event, query string) []string {
		t.Helper()
		rec := api.Do(http.MethodGet, eventPath(event)+"/participants"+query, token, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var b struct {
			Data []struct {
				User struct {
					Email string `json:"email"`
				} `json:"user"`
			} `json:"data"`
		}
		testutil.DecodeJSON(t, rec, &b)
		out := []string{}
		for _, p := range b.Data {
			out = append(out, p.User.Email)
		}
		return out
	}

	bootcamp := seed.EventBootcampOngoing
	assert.ElementsMatch(t, []string{emailMemberBE1, emailMemberFE}, participants(lead, bootcamp, ""))
	assert.ElementsMatch(t, []string{emailMemberBE1, emailMemberFE}, participants(headBE, bootcamp, ""), "own division event: everyone")
	assert.ElementsMatch(t, []string{emailMemberFE}, participants(headFE, bootcamp, ""), "other division event: own members only")
	assert.Empty(t, participants(be2, bootcamp, ""), "member sees only own registration")
	assert.ElementsMatch(t, []string{emailMemberBE2}, participants(be2, seed.EventStudyJamOngoing, ""))

	// Cancelled registrations are listed separately.
	require.Equal(t, http.StatusNoContent, api.Do(http.MethodDelete, eventPath(seed.EventStudyJamOngoing)+"/register", be2, nil).Code)
	assert.Empty(t, participants(lead, seed.EventStudyJamOngoing, ""))
	assert.ElementsMatch(t, []string{emailMemberBE2}, participants(lead, seed.EventStudyJamOngoing, "?status=cancelled"))

	assert.Equal(t, http.StatusNotFound, api.Do(http.MethodGet, eventPath(uuid.NewString())+"/participants", lead, nil).Code)
}
