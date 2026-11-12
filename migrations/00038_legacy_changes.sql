-- +goose Up
-- A retained latest-edit is source evidence, not a synthetic job or send ACK.
CREATE TABLE retained_message_edits (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 message_id text NOT NULL,
 source_key text NOT NULL,
 body text NOT NULL,
 mentioned_ids jsonb,
 PRIMARY KEY(account_id,whatsapp_chat,message_id)
);
CREATE TABLE legacy_change_projections (
 account_id text PRIMARY KEY REFERENCES legacy_imports(account_id),
 result jsonb NOT NULL
);
CREATE TRIGGER retained_message_edits_immutable BEFORE UPDATE OR DELETE ON retained_message_edits
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
CREATE TRIGGER legacy_change_projections_immutable BEFORE UPDATE OR DELETE ON legacy_change_projections
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
