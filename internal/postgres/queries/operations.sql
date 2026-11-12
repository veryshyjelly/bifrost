-- name: OperationCounts :one
SELECT (SELECT count(*) FROM bridge_bots b WHERE b.account_id=sqlc.arg(account_id)::text)::bigint AS bots,
(SELECT count(*) FROM bridge_routes r WHERE r.account_id=sqlc.arg(account_id)::text)::bigint AS conversations,
(SELECT count(*) FROM telegram_observations o WHERE o.account_id=sqlc.arg(account_id)::text AND NOT o.deleted AND o.cleanup_error<>'')::bigint AS cleanup_problems;
-- name: OperationJobCounts :many
SELECT state,count(*)::bigint AS count FROM jobs WHERE account_id=$1 GROUP BY state ORDER BY state;
-- name: OperationLastSync :one
SELECT p.payload FROM metadata_reports p JOIN metadata_runs r ON r.account_id=p.account_id AND r.run_id=p.run_id
WHERE p.account_id=$1 AND r.state='completed' ORDER BY (p.payload->>'FinishedAt')::bigint DESC,p.job_id DESC LIMIT 1;
-- name: OperationNoticeSettings :one
SELECT payload FROM operation_notice_settings WHERE account_id=$1;
-- name: SaveOperationNoticeSettings :exec
INSERT INTO operation_notice_settings(account_id,payload) VALUES($1,$2)
ON CONFLICT(account_id) DO UPDATE SET payload=EXCLUDED.payload;
