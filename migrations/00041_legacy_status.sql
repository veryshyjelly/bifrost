-- +goose Up
CREATE TABLE retained_status_feeds (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 management bigint NOT NULL,
 source_key text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,management)
);
CREATE TABLE retained_status_skips (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 source_key text NOT NULL,
 author text NOT NULL,
 skipped_at bigint NOT NULL,
 PRIMARY KEY(account_id,source_key)
);
CREATE TABLE retained_status_mute_operations (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 legacy_job_key text NOT NULL,
 source_key text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,legacy_job_key)
);
CREATE TABLE legacy_status_projections (
 account_id text PRIMARY KEY REFERENCES legacy_imports(account_id),
 result jsonb NOT NULL
);
CREATE TRIGGER retained_status_feeds_immutable BEFORE UPDATE OR DELETE ON retained_status_feeds FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
CREATE TRIGGER retained_status_skips_immutable BEFORE UPDATE OR DELETE ON retained_status_skips FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
CREATE TRIGGER retained_status_mute_operations_immutable BEFORE UPDATE OR DELETE ON retained_status_mute_operations FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
CREATE TRIGGER legacy_status_projections_immutable BEFORE UPDATE OR DELETE ON legacy_status_projections FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
