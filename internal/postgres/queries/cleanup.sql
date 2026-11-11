-- name: ScheduleControlExpiry :exec
INSERT INTO jobs(account_id,dedup,stream,kind,payload,due)
SELECT o.account_id,'control-expire:'||o.scope||':'||o.chat_id||':'||o.message_id,
 'control-expire:'||o.scope||':'||o.chat_id||':'||o.message_id,'control-expire',
 jsonb_build_object('scope',o.scope,'chat',o.chat_id,'message',o.message_id,'bot',CASE WHEN o.chat_id<0 THEN 'master' ELSE o.bot_id END),
 to_timestamp(o.expires_at::double precision/1000)
FROM telegram_observations o WHERE o.account_id=$1 AND o.scope=$2 AND o.chat_id=$3 AND o.message_id=$4
 AND o.expires_at>0 AND o.kind IN ('log','command') AND NOT o.deleted
ON CONFLICT(account_id,dedup) DO NOTHING;

-- name: CleanupObservation :one
SELECT * FROM telegram_observations WHERE account_id=$1 AND scope=$2 AND chat_id=$3 AND message_id=$4;

-- name: CleanupStep :one
SELECT payload FROM cleanup_steps WHERE account_id=$1 AND job_id=$2;

-- name: CreateCleanupStep :exec
INSERT INTO cleanup_steps(account_id,job_id,payload) VALUES($1,$2,$3);

-- name: UpdateCleanupStep :exec
UPDATE cleanup_steps SET payload=$3 WHERE account_id=$1 AND job_id=$2;

-- name: ClearRun :one
SELECT payload FROM control_clears WHERE account_id=$1 AND clear_id=$2 FOR UPDATE;

-- name: CreateClearRun :exec
INSERT INTO control_clears(account_id,clear_id,payload) VALUES($1,$2,$3);

-- name: UpdateClearRun :exec
UPDATE control_clears SET payload=$3 WHERE account_id=$1 AND clear_id=$2;

-- name: ClearCutoff :one
SELECT COALESCE(max(seq),0)::bigint FROM telegram_observations WHERE account_id=$1;

-- name: SnapshotClearItems :exec
INSERT INTO control_clear_items(account_id,clear_id,observation_seq)
SELECT o.account_id,sqlc.arg(clear_id),o.seq FROM telegram_observations o
WHERE o.account_id=sqlc.arg(account_id) AND o.scope=sqlc.arg(scope) AND o.chat_id=sqlc.arg(chat_id) AND o.thread_id=sqlc.arg(thread_id)
 AND o.seq<=sqlc.arg(cutoff) AND NOT o.deleted AND (sqlc.arg(mode)::text='all' OR o.kind IN ('log','command'));

-- name: ClearItemRows :many
SELECT o.* FROM control_clear_items i JOIN telegram_observations o ON o.account_id=i.account_id AND o.seq=i.observation_seq
WHERE i.account_id=$1 AND i.clear_id=$2 AND i.observation_seq>$3 ORDER BY i.observation_seq LIMIT $4;

-- name: MarkCleanupDeleted :exec
UPDATE telegram_observations SET deleted=true,cleanup_error='' WHERE account_id=$1 AND seq=ANY($2::bigint[]);

-- name: MarkCleanupBlocked :exec
UPDATE telegram_observations SET cleanup_error=$3 WHERE account_id=$1 AND seq=ANY($2::bigint[]) AND NOT deleted;

-- name: UnresolvedRelayInTarget :one
SELECT EXISTS(SELECT 1 FROM relay_plans p JOIN effects e ON e.account_id=p.account_id AND e.job_id=p.job_id
WHERE p.account_id=sqlc.arg(account_id) AND p.direction='in' AND e.state IN ('sending','uncertain')
 AND (p.payload->>'TelegramChat')::bigint=sqlc.arg(chat_id)::bigint AND (p.payload->>'ThreadID')::bigint=sqlc.arg(thread_id)::bigint
 AND (sqlc.arg(scope)::text='*' OR p.payload->>'BotID'=sqlc.arg(scope))) AS blocked;

-- name: IsRelayObservation :one
SELECT (EXISTS(SELECT 1 FROM message_mappings m WHERE m.account_id=sqlc.arg(account_id)::text AND m.scope=sqlc.arg(scope)::text AND m.telegram_chat_id=sqlc.arg(chat_id)::bigint AND m.telegram_message_id=sqlc.arg(message_id)::bigint)
OR EXISTS(SELECT 1 FROM relay_plans p JOIN effects e ON e.account_id=p.account_id AND e.job_id=p.job_id
WHERE p.account_id=sqlc.arg(account_id) AND p.direction='in' AND e.state='done'
 AND (p.payload->>'TelegramChat')::bigint=sqlc.arg(chat_id) AND (sqlc.arg(scope)::text='*' OR p.payload->>'BotID'=sqlc.arg(scope))
 AND (e.result->>'message_id')::bigint=sqlc.arg(message_id)::bigint))::boolean AS is_relay;
