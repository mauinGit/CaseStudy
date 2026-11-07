CREATE TYPE registration_status AS ENUM ('registered', 'cancelled');

CREATE TABLE event_registrations (
    id         UUID                PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id   UUID                NOT NULL REFERENCES events (id) ON DELETE RESTRICT,
    user_id    UUID                NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    status     registration_status NOT NULL DEFAULT 'registered',
    created_at TIMESTAMPTZ         NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ         NOT NULL DEFAULT now(),

    -- Prevents double registration even under concurrent requests.
    CONSTRAINT event_registrations_event_user_key UNIQUE (event_id, user_id)
);

-- (event_id, ...) lookups are served by the UNIQUE index above.
CREATE INDEX event_registrations_user_id_idx ON event_registrations (user_id);

CREATE TRIGGER event_registrations_set_updated_at
    BEFORE UPDATE ON event_registrations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
