-- +goose Up
CREATE TABLE retained_message_sources (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 message_id text NOT NULL,
 source_key text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,whatsapp_chat,message_id)
);
CREATE TABLE legacy_source_projections (
 account_id text PRIMARY KEY REFERENCES legacy_imports(account_id),
 result jsonb NOT NULL
);
CREATE TRIGGER retained_message_sources_immutable BEFORE UPDATE OR DELETE ON retained_message_sources
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
CREATE TRIGGER legacy_source_projections_immutable BEFORE UPDATE OR DELETE ON legacy_source_projections
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
