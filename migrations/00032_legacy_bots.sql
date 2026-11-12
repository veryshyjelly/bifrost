-- +goose Up
CREATE TABLE legacy_bot_projections (
 account_id text PRIMARY KEY REFERENCES legacy_imports(account_id),
 result jsonb NOT NULL
);
CREATE TABLE legacy_bot_logouts (
 account_id text NOT NULL REFERENCES legacy_imports(account_id),
 bot_id text NOT NULL,
 credential_revision text NOT NULL CHECK(credential_revision<>''),
 PRIMARY KEY(account_id,bot_id,credential_revision),
 FOREIGN KEY(account_id,bot_id) REFERENCES telegram_credentials(account_id,bot_id)
);
CREATE TRIGGER legacy_bot_projections_immutable BEFORE UPDATE OR DELETE ON legacy_bot_projections
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
CREATE TRIGGER legacy_bot_logouts_immutable BEFORE UPDATE OR DELETE ON legacy_bot_logouts
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
