CREATE TABLE audit_logs (
    id         BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- NULL = system action (e.g. seed).
    actor_id   UUID        NULL REFERENCES users (id) ON DELETE SET NULL,
    action     VARCHAR(64) NOT NULL,
    entity     VARCHAR(64) NOT NULL,
    entity_id  TEXT        NOT NULL,
    old_value  JSONB       NULL,
    new_value  JSONB       NULL,
    ip         INET        NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_created_at_idx ON audit_logs (created_at DESC, id DESC);
CREATE INDEX audit_logs_actor_id_idx   ON audit_logs (actor_id);
CREATE INDEX audit_logs_entity_idx     ON audit_logs (entity, entity_id);

-- Audit logs are append-only: block UPDATE and DELETE at the database level.
CREATE OR REPLACE FUNCTION audit_logs_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_logs is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_logs_append_only
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION audit_logs_block_mutation();
