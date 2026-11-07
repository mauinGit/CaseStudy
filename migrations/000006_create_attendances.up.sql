CREATE TABLE attendances (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id      UUID        NOT NULL REFERENCES events (id) ON DELETE RESTRICT,
    user_id       UUID        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    checked_in_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- One check-in per participant per event (idempotent check-in).
    CONSTRAINT attendances_event_user_key UNIQUE (event_id, user_id),
    -- A check-in can only exist for an existing registration.
    CONSTRAINT attendances_registration_fk FOREIGN KEY (event_id, user_id)
        REFERENCES event_registrations (event_id, user_id) ON DELETE RESTRICT
);

CREATE INDEX attendances_user_id_idx ON attendances (user_id);
