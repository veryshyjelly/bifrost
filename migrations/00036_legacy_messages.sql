-- +goose Up
ALTER TABLE message_mappings
 ADD COLUMN media boolean NOT NULL DEFAULT false,
 ADD COLUMN presentation jsonb,
 ADD COLUMN mentioned_ids jsonb,
 ADD COLUMN source_payload jsonb,
 ADD COLUMN receipt_origin text NOT NULL DEFAULT 'native-receipt';
CREATE TABLE legacy_message_projections (
 account_id text PRIMARY KEY REFERENCES legacy_imports(account_id),
 result jsonb NOT NULL
);
CREATE TRIGGER legacy_message_projections_immutable BEFORE UPDATE OR DELETE ON legacy_message_projections
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
