-- name: MetadataAdmission :one
SELECT run_id FROM metadata_admissions WHERE account_id=$1 AND request_key=$2;
-- name: MetadataJobByKey :one
SELECT id FROM jobs WHERE account_id=$1 AND dedup=$2;
-- name: CreateMetadataAdmission :exec
INSERT INTO metadata_admissions(account_id,request_key,run_id) VALUES($1,$2,$3);
-- name: MetadataActiveRuns :many
SELECT * FROM metadata_runs WHERE account_id=$1 AND session_id=$2 AND state<>'completed' ORDER BY ordinal;
-- name: MetadataRun :one
SELECT * FROM metadata_runs WHERE account_id=$1 AND run_id=$2;
-- name: CreateMetadataRun :exec
INSERT INTO metadata_runs(account_id,run_id,session_id,scope,source_id,source,state) VALUES($1,$2,$3,$4,$5,$6,'queued');
-- name: SetMetadataRunState :exec
UPDATE metadata_runs SET state=$3 WHERE account_id=$1 AND run_id=$2;
-- name: CreateMetadataRunRequest :exec
INSERT INTO metadata_run_requests(account_id,run_id,job_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING;
-- name: MetadataRunRequests :many
SELECT job_id FROM metadata_run_requests WHERE account_id=$1 AND run_id=$2 ORDER BY ordinal;
-- name: MetadataRunTargets :many
SELECT t.*,j.state AS job_state FROM metadata_run_targets t JOIN jobs j ON j.id=t.job_id WHERE t.account_id=$1 AND t.run_id=$2 ORDER BY t.job_id;
-- name: MetadataRunTarget :one
SELECT * FROM metadata_run_targets WHERE account_id=$1 AND job_id=$2;
-- name: CreateMetadataRunTarget :exec
INSERT INTO metadata_run_targets(account_id,run_id,kind,source_id,source_key,job_id) VALUES($1,$2,$3,$4,$5,$6);
-- name: CompleteMetadataRunTarget :exec
UPDATE metadata_run_targets SET outcome=$3,details=$4 WHERE account_id=$1 AND job_id=$2;
-- name: MetadataRoutes :many
SELECT * FROM bridge_routes WHERE account_id=$1 ORDER BY whatsapp_chat;
-- name: MetadataSourcePredecessor :one
SELECT EXISTS(SELECT 1 FROM metadata_run_targets t JOIN metadata_runs r ON r.account_id=t.account_id AND r.run_id=t.run_id JOIN jobs j ON j.id=t.job_id
WHERE t.account_id=sqlc.arg(account_id) AND r.session_id=sqlc.arg(session_id) AND t.kind=sqlc.arg(kind)
AND (t.source_id=ANY(sqlc.arg(aliases)::text[]) OR t.source_key=sqlc.arg(source_key)) AND j.state<>'done'
AND j.id<>sqlc.arg(job_id) AND (j.id<sqlc.arg(job_id) OR j.state='running'));
-- name: DeferMetadataJob :exec
UPDATE jobs SET state='pending',claim_token='',lease_until=NULL,due=clock_timestamp()+interval '1 second',attempts=GREATEST(attempts-1,0) WHERE id=$1;
-- name: MetadataJobEffects :many
SELECT * FROM effects WHERE account_id=$1 AND job_id=$2 ORDER BY effect_key;
-- name: CreateMetadataCronTick :exec
INSERT INTO metadata_cron_ticks(account_id,session_id,expression,minute,run_id) VALUES($1,$2,$3,$4,$5);
-- name: MetadataCronTick :one
SELECT run_id FROM metadata_cron_ticks WHERE account_id=$1 AND session_id=$2 AND expression=$3 AND minute=$4;
