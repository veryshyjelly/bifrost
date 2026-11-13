-- name: StatusFeed :one
SELECT n.account_id,n.management,n.job_id,n.payload,n.complete FROM status_feeds n WHERE n.account_id=$1 AND n.management=$2
UNION ALL SELECT r.account_id,r.management,0::bigint AS job_id,r.payload,true AS complete FROM retained_status_feeds r WHERE r.account_id=$1 AND r.management=$2
AND NOT EXISTS(SELECT 1 FROM status_feeds n WHERE n.account_id=r.account_id AND n.management=r.management);
-- name: CreateStatusFeed :exec
INSERT INTO status_feeds(account_id,management,job_id,payload) VALUES($1,$2,$3,$4);
-- name: CompleteStatusFeed :exec
UPDATE status_feeds SET payload=$3,complete=true WHERE account_id=$1 AND management=$2;
-- name: StatusPost :one
SELECT * FROM status_posts WHERE account_id=$1 AND post_id=$2;
-- name: CreateStatusPost :exec
INSERT INTO status_posts(account_id,post_id,job_id,payload,source,skipped) VALUES($1,$2,$3,$4,$5,$6);
-- name: CompleteStatusPost :exec
UPDATE status_posts SET complete=true WHERE account_id=$1 AND post_id=$2 AND job_id=$3;
-- name: StatusRefresh :one
SELECT * FROM status_refreshes WHERE account_id=$1 AND job_id=$2;
-- name: CreateStatusRefresh :exec
INSERT INTO status_refreshes(account_id,job_id,payload) VALUES($1,$2,$3);
-- name: CompleteStatusRefresh :exec
UPDATE status_refreshes SET complete=true WHERE account_id=$1 AND job_id=$2;
-- name: StatusMutes :many
SELECT payload FROM status_mutes WHERE account_id=$1 ORDER BY author;
-- name: SaveStatusMute :exec
INSERT INTO status_mutes(account_id,author,payload) VALUES($1,$2,$3)
ON CONFLICT(account_id,author) DO UPDATE SET payload=EXCLUDED.payload;
-- name: NextStatusMuteRevision :one
INSERT INTO status_mute_counters(account_id,revision) VALUES($1,1)
ON CONFLICT(account_id) DO UPDATE SET revision=status_mute_counters.revision+1 RETURNING revision;
-- name: StatusMuteOperation :one
SELECT payload FROM status_mute_operations WHERE account_id=$1 AND job_id=$2;
-- name: SaveStatusMuteOperation :exec
INSERT INTO status_mute_operations(account_id,job_id,payload) VALUES($1,$2,$3);
-- name: StatusControl :one
SELECT plan_key,payload FROM status_controls WHERE account_id=$1 AND control_id=$2;
-- name: CreateStatusControl :exec
INSERT INTO status_controls(account_id,control_id,plan_key,payload) VALUES($1,$2,$3,$4);
-- name: StatusControlEdit :one
SELECT payload,complete FROM status_control_edits WHERE account_id=$1 AND job_id=$2;
-- name: CreateStatusControlEdit :exec
INSERT INTO status_control_edits(account_id,job_id,payload) VALUES($1,$2,$3);
-- name: CompleteStatusControlEdit :exec
UPDATE status_control_edits SET complete=true WHERE account_id=$1 AND job_id=$2;
-- name: UpdateStatusControlEdit :exec
UPDATE status_control_edits SET payload=$3 WHERE account_id=$1 AND job_id=$2;
-- name: StatusTopicInUse :one
SELECT EXISTS(SELECT 1 FROM status_feeds f WHERE f.account_id=sqlc.arg(account_id) AND f.management=sqlc.arg(management)::bigint AND f.complete AND (f.payload->'Feed'->>'ThreadID')::bigint=sqlc.arg(thread_id)::bigint
UNION ALL SELECT 1 FROM retained_status_feeds r WHERE r.account_id=sqlc.arg(account_id) AND r.management=sqlc.arg(management)::bigint AND (r.payload->'Feed'->>'ThreadID')::bigint=sqlc.arg(thread_id)::bigint);
-- name: StatusTopicOrigins :many
SELECT * FROM status_feeds WHERE account_id=$1 AND job_id=$2;
