-- +goose Up
CREATE TABLE legacy_incoming_reaction_projections (
 account_id text PRIMARY KEY REFERENCES legacy_imports(account_id),
 result jsonb NOT NULL
);
-- Original fallback notices remain pending until their original job/notice
-- workflow is projected; retaining a reference never confirms a send.
CREATE TABLE legacy_pending_reaction_notices (
 account_id text NOT NULL REFERENCES legacy_imports(account_id),
 source_key text NOT NULL,
 whatsapp_chat text NOT NULL,
 message_id text NOT NULL,
 destination_key text NOT NULL,
 legacy_job_key text NOT NULL,
 PRIMARY KEY(account_id,source_key)
);
CREATE TRIGGER legacy_incoming_reaction_projections_immutable BEFORE UPDATE OR DELETE ON legacy_incoming_reaction_projections
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
CREATE TRIGGER legacy_pending_reaction_notices_immutable BEFORE UPDATE OR DELETE ON legacy_pending_reaction_notices
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
