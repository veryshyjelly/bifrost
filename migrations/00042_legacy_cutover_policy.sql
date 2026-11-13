-- +goose Up
CREATE TABLE legacy_cutover_policies (
 account_id text PRIMARY KEY REFERENCES legacy_imports(account_id),
 archive_digest text NOT NULL,
 queue_mode text NOT NULL CHECK(queue_mode='fresh'),
 recorded_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER legacy_cutover_policies_immutable BEFORE UPDATE OR DELETE ON legacy_cutover_policies
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
