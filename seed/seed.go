// Package seed inserts demo data (divisions, test accounts for every role and
// sample events). It is idempotent: rows use fixed IDs and ON CONFLICT DO
// NOTHING, so running it on every startup is safe.
package seed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/mauinGit/gdgoc-core-team-api/internal/domain"
)

// EmailDomain is the domain of every seeded test account.
const EmailDomain = "gdgoc-unsri.test"

// advisoryLockKey serialises concurrent seed runs (e.g. two instances booting).
const advisoryLockKey = 727_2026_11

// Fixed IDs keep the seed idempotent and make test data easy to reference.
const (
	DivBackend  = "10000000-0000-4000-8000-000000000001"
	DivFrontend = "10000000-0000-4000-8000-000000000002"
	DivMobile   = "10000000-0000-4000-8000-000000000003"
	DivUIUX     = "10000000-0000-4000-8000-000000000004"

	UserLead         = "20000000-0000-4000-8000-000000000001"
	UserHeadBackend  = "20000000-0000-4000-8000-000000000002"
	UserHeadFrontend = "20000000-0000-4000-8000-000000000003"
	UserMemberBE1    = "20000000-0000-4000-8000-000000000004"
	UserMemberBE2    = "20000000-0000-4000-8000-000000000005"
	UserMemberFE     = "20000000-0000-4000-8000-000000000006"
	UserMemberMobile = "20000000-0000-4000-8000-000000000007"
	UserMemberUIUX   = "20000000-0000-4000-8000-000000000008"

	EventBootcampOngoing  = "30000000-0000-4000-8000-000000000001"
	EventStudyJamOngoing  = "30000000-0000-4000-8000-000000000002"
	EventHackathonSmall   = "30000000-0000-4000-8000-000000000003"
	EventTechTalk         = "30000000-0000-4000-8000-000000000004"
	EventKickoffPast      = "30000000-0000-4000-8000-000000000005"
	EventDesignSprintFull = "30000000-0000-4000-8000-000000000006"
)

type Options struct {
	// Password used for every seeded account.
	Password   string
	BcryptCost int
	// Now is the reference time for event schedules (defaults to time.Now).
	Now func() time.Time
}

type division struct{ id, name string }

type user struct {
	id, name, email string
	role            domain.Role
	divisionID      *string
	phone, bio      string
}

type event struct {
	id, title, description, location string
	divisionID                       *string
	start, end                       time.Time
	quota                            int
	createdBy                        string
}

type registration struct {
	eventID, userID string
	checkedIn       bool
}

func ptr(s string) *string { return &s }

// Run seeds the database inside a single transaction.
func Run(ctx context.Context, pool *pgxpool.Pool, opts Options) error {
	if len(opts.Password) < 8 {
		return errors.New("seed password must be at least 8 characters")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	now := opts.Now().UTC().Truncate(time.Minute)

	hash, err := bcrypt.GenerateFromPassword([]byte(opts.Password), opts.BcryptCost)
	if err != nil {
		return fmt.Errorf("hash seed password: %w", err)
	}

	divisions := []division{
		{DivBackend, "Back-End"},
		{DivFrontend, "Front-End"},
		{DivMobile, "Mobile"},
		{DivUIUX, "UI/UX"},
	}

	users := []user{
		{UserLead, "Rizky Ramadhan", "lead@" + EmailDomain, domain.RoleLead, nil, "081200000001", "Lead GDGoC UNSRI"},
		{UserHeadBackend, "Dimas Saputra", "head.backend@" + EmailDomain, domain.RoleDivisionHead, ptr(DivBackend), "081200000002", "Head of Back-End"},
		{UserHeadFrontend, "Nabila Putri", "head.frontend@" + EmailDomain, domain.RoleDivisionHead, ptr(DivFrontend), "081200000003", "Head of Front-End"},
		{UserMemberBE1, "Andi Wijaya", "member.backend1@" + EmailDomain, domain.RoleMember, ptr(DivBackend), "081200000004", "Go enthusiast"},
		{UserMemberBE2, "Siti Aisyah", "member.backend2@" + EmailDomain, domain.RoleMember, ptr(DivBackend), "081200000005", "Database nerd"},
		{UserMemberFE, "Fajar Nugroho", "member.frontend@" + EmailDomain, domain.RoleMember, ptr(DivFrontend), "081200000006", "React developer"},
		{UserMemberMobile, "Putri Lestari", "member.mobile@" + EmailDomain, domain.RoleMember, ptr(DivMobile), "081200000007", "Flutter developer"},
		{UserMemberUIUX, "Bagas Pratama", "member.uiux@" + EmailDomain, domain.RoleMember, ptr(DivUIUX), "081200000008", "Product designer"},
	}

	day := 24 * time.Hour
	events := []event{
		{
			id: EventBootcampOngoing, title: "Back-End Bootcamp: Go REST API",
			description: "Bootcamp Back-End yang berjalan selama periode open recruitment (bisa dipakai untuk uji check-in).",
			location:    "Lab Komputer Fasilkom UNSRI", divisionID: ptr(DivBackend),
			start: now.Add(-2 * time.Hour), end: now.Add(60 * day), quota: 30, createdBy: UserHeadBackend,
		},
		{
			id: EventStudyJamOngoing, title: "Study Jam: Google Cloud Fundamentals",
			description: "Study jam umum untuk semua divisi (sedang berlangsung).",
			location:    "Online - Google Meet", divisionID: nil,
			start: now.Add(-1 * time.Hour), end: now.Add(60 * day), quota: 100, createdBy: UserLead,
		},
		{
			id: EventHackathonSmall, title: "Mini Hackathon Front-End",
			description: "Kuota kecil (2) untuk menguji QUOTA_FULL.",
			location:    "Ruang Seminar Fasilkom", divisionID: ptr(DivFrontend),
			start: now.Add(7 * day), end: now.Add(7*day + 8*time.Hour), quota: 2, createdBy: UserHeadFrontend,
		},
		{
			id: EventTechTalk, title: "Tech Talk: Career in Tech",
			description: "Tech talk umum bersama alumni.",
			location:    "Auditorium UNSRI", divisionID: nil,
			start: now.Add(14 * day), end: now.Add(14*day + 3*time.Hour), quota: 50, createdBy: UserLead,
		},
		{
			id: EventKickoffPast, title: "Kickoff Core Team 2026",
			description: "Event yang sudah selesai (pendaftaran & check-in harus ditolak).",
			location:    "Aula Fasilkom", divisionID: nil,
			start: now.Add(-30 * day), end: now.Add(-30*day + 3*time.Hour), quota: 80, createdBy: UserLead,
		},
		{
			id: EventDesignSprintFull, title: "UI/UX Design Sprint",
			description: "Kuota 1 dan sudah terisi (kuota penuh).",
			location:    "Co-working Space Palembang", divisionID: ptr(DivUIUX),
			start: now.Add(10 * day), end: now.Add(10*day + 6*time.Hour), quota: 1, createdBy: UserLead,
		},
	}

	registrations := []registration{
		{EventBootcampOngoing, UserMemberBE1, false},
		{EventStudyJamOngoing, UserMemberBE2, false},
		{EventHackathonSmall, UserMemberFE, false},
		{EventDesignSprintFull, UserMemberUIUX, false},
		{EventKickoffPast, UserMemberBE1, true},
		{EventKickoffPast, UserMemberFE, true},
	}

	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, advisoryLockKey); err != nil {
			return fmt.Errorf("acquire seed lock: %w", err)
		}

		for _, d := range divisions {
			if _, err := tx.Exec(ctx,
				`INSERT INTO divisions (id, name) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				d.id, d.name); err != nil {
				return fmt.Errorf("seed division %s: %w", d.name, err)
			}
		}

		for _, u := range users {
			if _, err := tx.Exec(ctx,
				`INSERT INTO users (id, name, email, password_hash, role, division_id, phone, bio)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				 ON CONFLICT DO NOTHING`,
				u.id, u.name, u.email, string(hash), string(u.role), u.divisionID, u.phone, u.bio); err != nil {
				return fmt.Errorf("seed user %s: %w", u.email, err)
			}
		}

		for _, e := range events {
			if _, err := tx.Exec(ctx,
				`INSERT INTO events (id, title, description, division_id, location, start_at, end_at, quota, created_by)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				 ON CONFLICT DO NOTHING`,
				e.id, e.title, e.description, e.divisionID, e.location, e.start, e.end, e.quota, e.createdBy); err != nil {
				return fmt.Errorf("seed event %s: %w", e.title, err)
			}
		}

		for _, r := range registrations {
			if err := seedRegistration(ctx, tx, r); err != nil {
				return err
			}
		}
		return nil
	})
}

// seedRegistration inserts a registration and bumps registered_count only when
// the row is new, keeping the counter consistent across repeated runs.
func seedRegistration(ctx context.Context, tx pgx.Tx, r registration) error {
	tag, err := tx.Exec(ctx,
		`INSERT INTO event_registrations (event_id, user_id) VALUES ($1, $2)
		 ON CONFLICT (event_id, user_id) DO NOTHING`,
		r.eventID, r.userID)
	if err != nil {
		return fmt.Errorf("seed registration %s/%s: %w", r.eventID, r.userID, err)
	}
	if tag.RowsAffected() == 1 {
		if _, err := tx.Exec(ctx,
			`UPDATE events SET registered_count = registered_count + 1 WHERE id = $1`,
			r.eventID); err != nil {
			return fmt.Errorf("seed registered_count %s: %w", r.eventID, err)
		}
	}
	if r.checkedIn {
		if _, err := tx.Exec(ctx,
			`INSERT INTO attendances (event_id, user_id, checked_in_at)
			 SELECT id, $2, start_at + interval '10 minutes' FROM events WHERE id = $1
			 ON CONFLICT (event_id, user_id) DO NOTHING`,
			r.eventID, r.userID); err != nil {
			return fmt.Errorf("seed attendance %s/%s: %w", r.eventID, r.userID, err)
		}
	}
	return nil
}
