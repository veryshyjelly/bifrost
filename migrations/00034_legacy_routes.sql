-- +goose Up
CREATE TABLE legacy_route_projections (
 account_id text PRIMARY KEY REFERENCES legacy_imports(account_id),
 result jsonb NOT NULL
);
CREATE TRIGGER legacy_route_projections_immutable BEFORE UPDATE OR DELETE ON legacy_route_projections
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
