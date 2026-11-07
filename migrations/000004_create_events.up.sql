CREATE TABLE events (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    title            VARCHAR(150) NOT NULL,
    description      TEXT         NOT NULL DEFAULT '',
    -- NULL = general event open to every division.
    division_id      UUID         NULL REFERENCES divisions (id) ON DELETE RESTRICT,
    location         VARCHAR(200) NOT NULL,
    start_at         TIMESTAMPTZ  NOT NULL,
    end_at           TIMESTAMPTZ  NOT NULL,
    quota            INTEGER      NOT NULL,
    registered_count INTEGER      NOT NULL DEFAULT 0,
    created_by       UUID         NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ  NULL,

    CONSTRAINT events_quota_positive_chk        CHECK (quota > 0),
    CONSTRAINT events_registered_count_min_chk  CHECK (registered_count >= 0),
    -- Last line of defence: the DB itself refuses to exceed the quota.
    CONSTRAINT events_registered_count_max_chk  CHECK (registered_count <= quota),
    CONSTRAINT events_time_range_chk            CHECK (end_at > start_at)
);

CREATE INDEX events_start_at_idx    ON events (start_at)    WHERE deleted_at IS NULL;
CREATE INDEX events_division_id_idx ON events (division_id) WHERE deleted_at IS NULL;
CREATE INDEX events_created_by_idx  ON events (created_by);
CREATE INDEX events_deleted_at_idx  ON events (deleted_at);

CREATE TRIGGER events_set_updated_at
    BEFORE UPDATE ON events
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
