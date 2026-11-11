-- +goose Up
CREATE TABLE metadata_picture_assets (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 asset_hash text NOT NULL CHECK(asset_hash ~ '^[0-9a-f]{64}$'),
 jpeg bytea NOT NULL CHECK(octet_length(jpeg)>0 AND octet_length(jpeg)<=8388608),
 PRIMARY KEY(account_id,asset_hash)
);
CREATE TABLE metadata_picture_plans (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,job_id)
);
CREATE TABLE metadata_picture_cache (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 scope_key text NOT NULL,
 source_hash text NOT NULL CHECK(source_hash ~ '^[0-9a-f]{64}$'),
 photo_id text NOT NULL CHECK(photo_id<>''),
 job_id bigint NOT NULL REFERENCES jobs(id),
 PRIMARY KEY(account_id,scope_key)
);
-- Forward only: frozen assets, upload receipts and scoped cache evidence persist.
