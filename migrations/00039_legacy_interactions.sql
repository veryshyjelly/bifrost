-- +goose Up
CREATE TABLE retained_reaction_choices (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 source_key text NOT NULL,
 emoji text NOT NULL,
 supported boolean NOT NULL,
 PRIMARY KEY(account_id,source_key)
);
CREATE TABLE legacy_interaction_projections (
 account_id text PRIMARY KEY REFERENCES legacy_imports(account_id),
 result jsonb NOT NULL
);
CREATE TRIGGER retained_reaction_choices_immutable BEFORE UPDATE OR DELETE ON retained_reaction_choices
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
CREATE TRIGGER legacy_interaction_projections_immutable BEFORE UPDATE OR DELETE ON legacy_interaction_projections
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
