-- +goose Up
CREATE TABLE legacy_asset_verifications (
 account_id text PRIMARY KEY REFERENCES legacy_imports(account_id),
 archive_digest text NOT NULL CHECK(archive_digest ~ '^[a-f0-9]{64}$'),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 verified_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER legacy_asset_verifications_immutable BEFORE UPDATE OR DELETE ON legacy_asset_verifications
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
