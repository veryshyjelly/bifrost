-- name: LockIdentity :exec
SELECT pg_advisory_xact_lock(725104321);

-- name: Identity :one
SELECT account_id, key_fingerprint FROM bridge_identity WHERE singleton = true;

-- name: CreateIdentity :exec
INSERT INTO bridge_identity (account_id, key_fingerprint) VALUES ($1, $2);

-- name: Enqueue :one
WITH gate AS (
  SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(account_id)::text || ':' || sqlc.arg(stream)::text, 0))
)
INSERT INTO jobs (account_id, dedup, stream, kind, payload)
SELECT sqlc.arg(account_id), sqlc.arg(dedup), sqlc.arg(stream), sqlc.arg(kind), sqlc.arg(payload) FROM gate
ON CONFLICT (account_id, dedup) DO NOTHING
RETURNING id;

-- name: Claim :one
WITH candidate AS (
  SELECT j.id FROM jobs j
  WHERE j.state = 'pending' AND j.due <= clock_timestamp()
    AND NOT EXISTS (
      SELECT 1 FROM jobs p WHERE p.account_id = j.account_id AND p.stream = j.stream
        AND p.id < j.id AND p.state <> 'done'
    )
  ORDER BY CASE
    WHEN j.kind = 'history' OR j.kind LIKE 'history-%' THEN 3
    WHEN j.kind LIKE 'control-%' THEN 2
    WHEN j.kind LIKE 'sync-%' OR j.kind LIKE 'status-%' THEN 1
    ELSE 0 END, j.id
  FOR UPDATE OF j SKIP LOCKED LIMIT 1
)
UPDATE jobs SET state = 'running', attempts = attempts + 1,
  claim_token = sqlc.arg(token), lease_until = clock_timestamp() + sqlc.arg(lease_seconds)::integer * interval '1 second'
FROM candidate WHERE jobs.id = candidate.id RETURNING jobs.*;

-- name: LockClaim :one
SELECT * FROM jobs WHERE id = $1 AND claim_token = $2 AND state = 'running'
  AND lease_until > clock_timestamp() FOR UPDATE;

-- name: Renew :execrows
UPDATE jobs SET lease_until = clock_timestamp() + sqlc.arg(lease_seconds)::integer * interval '1 second'
WHERE id = sqlc.arg(id) AND claim_token = sqlc.arg(claim_token) AND state = 'running' AND lease_until > clock_timestamp();

-- name: SetJobState :exec
UPDATE jobs SET state = sqlc.arg(state), claim_token = '', lease_until = NULL,
 due = clock_timestamp() + sqlc.arg(delay_seconds)::integer * interval '1 second' WHERE id = sqlc.arg(id);

-- name: UnfinishedEffects :one
SELECT count(*) FROM effects WHERE job_id = $1 AND state <> 'done';

-- name: GetEffect :one
SELECT * FROM effects WHERE account_id = $1 AND effect_key = $2 FOR UPDATE;

-- name: CreateEffect :exec
INSERT INTO effects(account_id, effect_key, job_id, state, request)
VALUES ($1, $2, $3, 'pending', $4) ON CONFLICT DO NOTHING;

-- name: StartEffect :exec
UPDATE effects SET state = 'sending' WHERE account_id = $1 AND effect_key = $2 AND state = 'pending';

-- name: ResolveEffect :execrows
UPDATE effects SET state = sqlc.arg(state), result = sqlc.narg(result)
WHERE account_id = sqlc.arg(account_id) AND effect_key = sqlc.arg(effect_key)
  AND job_id = sqlc.arg(job_id) AND state = 'sending';

-- name: ExpiredJobs :many
SELECT * FROM jobs WHERE state = 'running' AND lease_until <= clock_timestamp()
ORDER BY id FOR UPDATE SKIP LOCKED;

-- name: MarkSendingUncertain :exec
UPDATE effects SET state = 'uncertain' WHERE job_id = $1 AND state = 'sending';

-- name: HasUncertainEffect :one
SELECT EXISTS(SELECT 1 FROM effects WHERE job_id = $1 AND state = 'uncertain');

-- name: ReviewJob :one
SELECT * FROM jobs WHERE id = $1 AND state IN ('failed', 'uncertain') FOR UPDATE;

-- name: ResetUncertainEffects :exec
UPDATE effects SET state = 'pending' WHERE job_id = $1 AND state = 'uncertain';

-- name: JobCounts :many
SELECT state, count(*) FROM jobs GROUP BY state ORDER BY state;

-- name: HasSendingEffect :one
SELECT EXISTS(SELECT 1 FROM effects WHERE job_id = $1 AND state = 'sending');
