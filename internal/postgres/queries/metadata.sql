-- name: MetadataTextPlan :one
SELECT payload FROM metadata_text_plans WHERE account_id=$1 AND job_id=$2;
-- name: CreateMetadataTextPlan :exec
INSERT INTO metadata_text_plans(account_id,job_id,payload) VALUES($1,$2,$3);
-- name: SaveMetadataTextPlan :exec
UPDATE metadata_text_plans SET payload=$3 WHERE account_id=$1 AND job_id=$2;
-- name: MetadataSnapshot :one
SELECT job_id,fetched_at,payload FROM metadata_snapshots WHERE account_id=$1 AND session_id=$2 AND source_id=$3;
-- name: SaveMetadataSnapshot :execrows
INSERT INTO metadata_snapshots(account_id,session_id,source_id,job_id,fetched_at,payload) VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(account_id,session_id,source_id) DO UPDATE SET job_id=EXCLUDED.job_id,fetched_at=EXCLUDED.fetched_at,payload=EXCLUDED.payload
WHERE (metadata_snapshots.fetched_at,metadata_snapshots.job_id)<=(EXCLUDED.fetched_at,EXCLUDED.job_id);
-- name: RenameMetadataRoute :exec
UPDATE bridge_routes SET name=$3 WHERE account_id=$1 AND whatsapp_chat=$2;
-- name: MetadataTopicName :one
SELECT name FROM metadata_topic_names WHERE account_id=$1 AND session_id=$2 AND source_id=$3 AND chat_id=$4 AND thread_id=$5 AND return_key=$6;
-- name: RememberMetadataTopicName :exec
INSERT INTO metadata_topic_names(account_id,session_id,source_id,chat_id,thread_id,return_key,name) VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT DO NOTHING;
-- name: SetMetadataTopicName :exec
UPDATE metadata_topic_names SET name=$7 WHERE account_id=$1 AND session_id=$2 AND source_id=$3 AND chat_id=$4 AND thread_id=$5 AND return_key=$6;
