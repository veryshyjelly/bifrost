-- name: MetadataPicturePlan :one
SELECT payload FROM metadata_picture_plans WHERE account_id=$1 AND job_id=$2;
-- name: CreateMetadataPicturePlan :exec
INSERT INTO metadata_picture_plans(account_id,job_id,payload) VALUES($1,$2,$3);
-- name: SaveMetadataPicturePlan :exec
UPDATE metadata_picture_plans SET payload=$3 WHERE account_id=$1 AND job_id=$2;
-- name: MetadataPictureAsset :one
SELECT jpeg FROM metadata_picture_assets WHERE account_id=$1 AND asset_hash=$2;
-- name: CreateMetadataPictureAsset :exec
INSERT INTO metadata_picture_assets(account_id,asset_hash,jpeg) VALUES($1,$2,$3) ON CONFLICT DO NOTHING;
-- name: MetadataPictureCache :one
SELECT source_hash,photo_id FROM metadata_picture_cache WHERE account_id=$1 AND scope_key=$2;
-- name: SaveMetadataPictureCache :exec
INSERT INTO metadata_picture_cache(account_id,scope_key,source_hash,photo_id,job_id) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(account_id,scope_key) DO UPDATE SET source_hash=EXCLUDED.source_hash,photo_id=EXCLUDED.photo_id,job_id=EXCLUDED.job_id;
