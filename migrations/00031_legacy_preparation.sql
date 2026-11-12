-- +goose Up
-- A prepared import is deliberately not runnable. All source rows and their
-- exact strings survive until active journal projection and asset restoration
-- have been validated. No source sending effect is adopted by preparation.
CREATE TABLE legacy_imports (
 account_id text PRIMARY KEY REFERENCES bridge_identity(account_id),
 archive_digest text NOT NULL UNIQUE CHECK(archive_digest ~ '^[a-f0-9]{64}$'),
 sqlite_digest text NOT NULL CHECK(sqlite_digest ~ '^[a-f0-9]{64}$'),
 manifest jsonb NOT NULL,
 configuration_ciphertext text NOT NULL CHECK(configuration_ciphertext<>''),
 report jsonb NOT NULL,
 prepared_at timestamptz NOT NULL DEFAULT now(),
 activated_at timestamptz
);
CREATE TABLE legacy_rows (
 account_id text NOT NULL REFERENCES legacy_imports(account_id),
 ordinal bigint NOT NULL CHECK(ordinal>0),
 source_table text NOT NULL CHECK(source_table IN ('kv','jobs','effects','messages','aliases','telegram_messages')),
 raw_row bytea NOT NULL,
 PRIMARY KEY(account_id,ordinal)
);
CREATE INDEX legacy_rows_table ON legacy_rows(account_id,source_table,ordinal);
CREATE TABLE legacy_values (
 account_id text NOT NULL REFERENCES legacy_imports(account_id),
 kind text NOT NULL CHECK(kind<>''),
 source_id text NOT NULL CHECK(source_id<>''),
 source_value text NOT NULL,
 PRIMARY KEY(account_id,kind,source_id)
);
-- Append-only source evidence. Projection uses separate native journals.
-- +goose StatementBegin
CREATE FUNCTION protect_legacy_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'legacy source evidence is immutable';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER legacy_rows_immutable BEFORE UPDATE OR DELETE ON legacy_rows
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
CREATE TRIGGER legacy_values_immutable BEFORE UPDATE OR DELETE ON legacy_values
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
-- +goose StatementBegin
CREATE FUNCTION protect_legacy_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.account_id IS DISTINCT FROM OLD.account_id
 OR NEW.archive_digest IS DISTINCT FROM OLD.archive_digest
 OR NEW.sqlite_digest IS DISTINCT FROM OLD.sqlite_digest
 OR NEW.manifest IS DISTINCT FROM OLD.manifest
 OR NEW.configuration_ciphertext IS DISTINCT FROM OLD.configuration_ciphertext
 OR NEW.prepared_at IS DISTINCT FROM OLD.prepared_at
 OR (OLD.activated_at IS NOT NULL AND NEW.activated_at IS DISTINCT FROM OLD.activated_at) THEN
  RAISE EXCEPTION 'legacy checkpoint identity and source evidence are immutable';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER legacy_checkpoint_immutable BEFORE UPDATE ON legacy_imports
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_checkpoint();
