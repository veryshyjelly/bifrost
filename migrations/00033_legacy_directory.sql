-- +goose Up
-- Keep the legacy selected profile separately from canonical routing identity.
-- A confirmed native metadata refresh can replace this initial preference.
ALTER TABLE bridge_contact_bots ADD COLUMN source_profile text NOT NULL DEFAULT '';
CREATE TABLE legacy_directory_projections (
 account_id text PRIMARY KEY REFERENCES legacy_imports(account_id),
 result jsonb NOT NULL
);
CREATE TRIGGER legacy_directory_projections_immutable BEFORE UPDATE OR DELETE ON legacy_directory_projections
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
