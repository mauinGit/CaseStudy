CREATE TYPE user_role AS ENUM ('lead', 'division_head', 'member');

CREATE TABLE users (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    name          VARCHAR(100) NOT NULL,
    -- Always stored lowercase (see CHECK below), so UNIQUE(email) is case-insensitive.
    email         VARCHAR(254) NOT NULL,
    -- NULL for accounts that only sign in with Google.
    password_hash TEXT         NULL,
    google_sub    VARCHAR(255) NULL,
    role          user_role    NOT NULL DEFAULT 'member',
    division_id   UUID         NULL REFERENCES divisions (id) ON DELETE RESTRICT,
    phone         VARCHAR(20)  NULL,
    bio           VARCHAR(500) NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ  NULL,

    CONSTRAINT users_email_key UNIQUE (email),
    CONSTRAINT users_google_sub_key UNIQUE (google_sub),
    CONSTRAINT users_email_lowercase_chk CHECK (email = lower(email)),
    -- Only the Lead may exist without a division.
    CONSTRAINT users_division_required_chk CHECK (role = 'lead' OR division_id IS NOT NULL)
);

CREATE INDEX users_division_id_idx ON users (division_id) WHERE deleted_at IS NULL;
CREATE INDEX users_role_idx        ON users (role)        WHERE deleted_at IS NULL;
CREATE INDEX users_deleted_at_idx  ON users (deleted_at);
CREATE INDEX users_created_at_idx  ON users (created_at)  WHERE deleted_at IS NULL;
CREATE INDEX users_name_idx        ON users (name)        WHERE deleted_at IS NULL;
-- Trigram indexes make "ILIKE '%term%'" searches index-assisted.
CREATE INDEX users_name_trgm_idx   ON users USING gin (name gin_trgm_ops);
CREATE INDEX users_email_trgm_idx  ON users USING gin (email gin_trgm_ops);

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
