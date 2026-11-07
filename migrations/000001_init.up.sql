-- Trigram index support for ILIKE search on name/email.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Keeps updated_at in sync on every UPDATE.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
