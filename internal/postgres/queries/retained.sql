-- Read-only compatibility with applied schema history; never rewrite its identifiers.
-- name: RetainedBotLogout :one
SELECT EXISTS(SELECT 1 FROM legacy_bot_logouts WHERE account_id=$1 AND bot_id=$2 AND credential_revision=$3);
-- name: RetainedStatusMuteOperation :one
SELECT payload FROM retained_status_mute_operations WHERE account_id=sqlc.arg(account_id) AND legacy_job_key=sqlc.arg(retained_job_key);
-- name: RetainedStatusSkipped :one
SELECT EXISTS(SELECT 1 FROM retained_status_skips WHERE account_id=$1 AND source_key=$2);
-- name: StatusReplySource :one
SELECT p.source FROM status_posts p WHERE p.account_id=$1 AND p.post_id=$2
UNION ALL SELECT s.payload FROM retained_message_sources s WHERE s.account_id=$1 AND s.whatsapp_chat='status@broadcast' AND s.message_id=$2 AND s.payload->>'Status'='true'
AND NOT EXISTS(SELECT 1 FROM status_posts p WHERE p.account_id=s.account_id AND p.post_id=s.message_id);
-- name: RetainedActivationPending :one
SELECT EXISTS(SELECT 1 FROM legacy_imports WHERE account_id=$1 AND activated_at IS NULL);
-- name: RetainedReactionChoice :one
SELECT emoji,supported FROM retained_reaction_choices WHERE account_id=$1 AND source_key=$2;
-- name: RetainedRuntimeConfiguration :one
SELECT account_id,configuration_ciphertext FROM legacy_imports x WHERE x.account_id=$1;

-- name: MessageComplete :one
SELECT EXISTS(SELECT 1 FROM source_completions WHERE account_id=$1 AND whatsapp_chat=$2 AND message_id=$3);


-- name: SourceActorIsOwn :one
SELECT EXISTS(SELECT 1 FROM whatsapp_sources w WHERE w.account_id=sqlc.arg(account_id)
 AND (w.whatsapp_chat LIKE '%@g.us' OR w.whatsapp_chat='status@broadcast')
 AND w.payload->>'Own'='true' AND w.payload->>'SenderID'=ANY(sqlc.arg(identities)::text[]))
OR EXISTS(SELECT 1 FROM retained_message_sources s WHERE s.account_id=sqlc.arg(account_id)
 AND (s.whatsapp_chat LIKE '%@g.us' OR s.whatsapp_chat='status@broadcast')
 AND s.payload->>'Own'='true' AND s.payload->>'SenderID'=ANY(sqlc.arg(identities)::text[]))
OR EXISTS(SELECT 1 FROM history_items i JOIN history_runs r ON r.account_id=i.account_id AND r.run_id=i.run_id
 WHERE i.account_id=sqlc.arg(account_id) AND r.whatsapp_chat LIKE '%@g.us'
 AND i.payload->'Source'->>'Own'='true' AND i.payload->'Source'->>'SenderID'=ANY(sqlc.arg(identities)::text[]));
