-- name: LegacyStatusProjection :one
SELECT result FROM legacy_status_projections WHERE account_id=$1;
-- name: SaveLegacyStatusProjection :exec
INSERT INTO legacy_status_projections(account_id,result) VALUES($1,$2);
-- name: LegacyStatusValues :many
SELECT kind,source_id,source_value FROM legacy_values WHERE account_id=$1 AND kind IN ('status-feed','status-mute','status-mute-counter','status-mute-op','status-skipped') ORDER BY kind,source_id;
-- name: LegacyStatusJobs :many
SELECT raw_row FROM legacy_rows WHERE account_id=$1 AND source_table='jobs' ORDER BY ordinal;
-- name: LegacyStatusConflict :one
SELECT EXISTS(SELECT 1 FROM status_feeds f WHERE f.account_id=$1
 UNION ALL SELECT 1 FROM retained_status_feeds r WHERE r.account_id=$1
 UNION ALL SELECT 1 FROM status_mutes m WHERE m.account_id=$1
 UNION ALL SELECT 1 FROM status_mute_counters c WHERE c.account_id=$1
 UNION ALL SELECT 1 FROM status_mute_operations o WHERE o.account_id=$1
 UNION ALL SELECT 1 FROM retained_status_skips s WHERE s.account_id=$1
 UNION ALL SELECT 1 FROM retained_status_mute_operations p WHERE p.account_id=$1);
-- name: SaveRetainedStatusFeed :exec
INSERT INTO retained_status_feeds(account_id,management,source_key,payload) VALUES($1,$2,$3,$4);
-- name: SaveRetainedStatusSkip :exec
INSERT INTO retained_status_skips(account_id,source_key,author,skipped_at) VALUES($1,$2,$3,$4);
-- name: SaveRetainedStatusMuteOperation :exec
INSERT INTO retained_status_mute_operations(account_id,legacy_job_key,source_key,payload) VALUES($1,$2,$3,$4);
-- name: RetainedStatusMuteOperation :one
SELECT payload FROM retained_status_mute_operations WHERE account_id=$1 AND legacy_job_key=$2;
-- name: RetainedStatusSkipped :one
SELECT EXISTS(SELECT 1 FROM retained_status_skips WHERE account_id=$1 AND source_key=$2);
-- name: SaveLegacyStatusCounter :exec
INSERT INTO status_mute_counters(account_id,revision) VALUES($1,$2);
-- name: StatusReplySource :one
SELECT p.source FROM status_posts p WHERE p.account_id=$1 AND p.post_id=$2
UNION ALL SELECT s.payload FROM retained_message_sources s WHERE s.account_id=$1 AND s.whatsapp_chat='status@broadcast' AND s.message_id=$2 AND s.payload->>'Status'='true'
AND NOT EXISTS(SELECT 1 FROM status_posts p WHERE p.account_id=s.account_id AND p.post_id=s.message_id);
