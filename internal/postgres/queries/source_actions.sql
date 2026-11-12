-- name: CreateSourceAction :exec
INSERT INTO source_action_jobs(account_id,job_id,source_job_id,payload) VALUES($1,$2,$3,$4);
-- name: SourceActionJob :one
SELECT * FROM source_action_jobs WHERE account_id=$1 AND job_id=$2;
-- name: SourceActionOrigin :one
SELECT * FROM source_action_jobs WHERE account_id=$1 AND source_job_id=$2;
-- name: UpdateSourceAction :exec
UPDATE source_action_jobs SET payload=$3 WHERE account_id=$1 AND job_id=$2;
