-- +goose Up
-- Explicit source completion evidence can survive a port without retaining a
-- provider request/receipt. Never infer these rows from partial mappings.
CREATE TABLE source_completions (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL CHECK(whatsapp_chat<>''),
 message_id text NOT NULL CHECK(message_id<>''),
 source_key text NOT NULL CHECK(source_key<>''),
 PRIMARY KEY(account_id,whatsapp_chat,message_id)
);
CREATE TABLE legacy_completion_projections (
 account_id text PRIMARY KEY REFERENCES legacy_imports(account_id),
 result jsonb NOT NULL
);
CREATE TRIGGER source_completions_immutable BEFORE UPDATE OR DELETE ON source_completions
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
CREATE TRIGGER legacy_completion_projections_immutable BEFORE UPDATE OR DELETE ON legacy_completion_projections
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
