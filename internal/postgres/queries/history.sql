-- name: HistoryChoice :one
SELECT payload FROM history_choices WHERE account_id=$1 AND choice_id=$2;
-- name: SaveHistoryChoice :exec
INSERT INTO history_choices(account_id,choice_id,payload) VALUES($1,$2,$3)
ON CONFLICT(account_id,choice_id) DO UPDATE SET payload=EXCLUDED.payload;
-- name: ActiveHistoryRun :one
SELECT payload FROM history_runs WHERE account_id=$1 AND session_id=$2 AND whatsapp_chat=$3 AND state IN ('queued','fetching','delivering');
-- name: HistoryRun :one
SELECT state,payload FROM history_runs WHERE account_id=$1 AND run_id=$2;
-- name: CreateHistoryRun :exec
INSERT INTO history_runs(account_id,run_id,session_id,whatsapp_chat,state,payload) VALUES($1,$2,$3,$4,'queued',$5);
-- name: UpdateHistoryRun :exec
UPDATE history_runs SET state=$3,payload=$4 WHERE account_id=$1 AND run_id=$2;
-- name: CreateHistoryItem :exec
INSERT INTO history_items(account_id,run_id,item_index,job_id,source_id,payload) VALUES($1,$2,$3,$4,$5,$6);
-- name: HistoryItem :one
SELECT * FROM history_items WHERE account_id=$1 AND job_id=$2;
-- name: HistoryItems :many
SELECT h.*,j.state AS job_state FROM history_items h JOIN jobs j ON j.id=h.job_id WHERE h.account_id=$1 AND h.run_id=$2 ORDER BY item_index;
-- name: UpdateHistoryItem :exec
UPDATE history_items SET state=$3,payload=$4,plan_key=$5 WHERE account_id=$1 AND job_id=$2;
-- name: HistoryDestinationDelivered :one
SELECT EXISTS(SELECT 1 FROM message_mappings m LEFT JOIN relay_plans p ON p.account_id=m.account_id AND p.plan_key=m.plan_key
LEFT JOIN telegram_observations o ON o.account_id=m.account_id AND o.scope=m.scope AND o.chat_id=m.telegram_chat_id AND o.message_id=m.telegram_message_id
WHERE m.account_id=sqlc.arg(account_id) AND m.whatsapp_chat=sqlc.arg(whatsapp_chat) AND m.whatsapp_message_id=sqlc.arg(message_id)::text
AND m.telegram_chat_id=sqlc.arg(chat_id)::bigint AND m.thread_id=sqlc.arg(thread_id)::bigint AND m.scope=sqlc.arg(scope)::text
AND NOT m.deleted AND NOT COALESCE(o.deleted,false) AND COALESCE(o.thread_id,m.thread_id)=sqlc.arg(thread_id)::bigint
AND (COALESCE(p.complete,false) OR (m.direction='out' AND m.receipt_origin='native-receipt')
 OR (m.plan_key IS NULL AND m.receipt_origin<>'native-receipt' AND (
  EXISTS(SELECT 1 FROM legacy_values v WHERE v.account_id=m.account_id AND v.kind='destination-complete' AND v.source_id=sqlc.arg(retained_destination_key)::text AND v.source_value::jsonb='true'::jsonb)
  OR (NOT EXISTS(SELECT 1 FROM legacy_values v WHERE v.account_id=m.account_id AND v.kind='destination-start' AND v.source_id=sqlc.arg(retained_destination_key)::text)
   AND (m.direction='out' OR EXISTS(SELECT 1 FROM source_completions c WHERE c.account_id=m.account_id AND c.whatsapp_chat=m.whatsapp_chat AND c.message_id=m.whatsapp_message_id)))
 )))
AND NOT EXISTS(SELECT 1 FROM retired_topics t WHERE t.account_id=m.account_id AND t.telegram_chat_id=m.telegram_chat_id AND t.thread_id=m.thread_id));
-- name: HistoryMessageRevoked :one
SELECT EXISTS(SELECT 1 FROM message_tombstones WHERE account_id=$1 AND whatsapp_chat=$2 AND message_id=$3 AND revoked);

-- name: HistoryPartialPlans :many
SELECT * FROM relay_plans WHERE account_id=sqlc.arg(account_id) AND whatsapp_chat=sqlc.arg(whatsapp_chat) AND direction='in' AND NOT complete
AND payload->>'WhatsAppMessage'=sqlc.arg(message_id)::text AND (payload->>'TelegramChat')::bigint=sqlc.arg(chat_id)::bigint AND (payload->>'ThreadID')::bigint=sqlc.arg(thread_id)::bigint
ORDER BY job_id;
-- name: LockHistoryPartialJob :one
SELECT * FROM jobs WHERE id=$1 FOR UPDATE NOWAIT;
-- name: HistoryPartialPlanCount :one
SELECT count(*) FROM relay_plans WHERE job_id=$1;
-- name: AdoptHistoryPartialEffects :exec
UPDATE effects SET job_id=sqlc.arg(new_job_id) WHERE account_id=sqlc.arg(account_id) AND job_id=sqlc.arg(old_job_id);
-- name: AdoptHistoryPartialPlan :exec
UPDATE relay_plans SET job_id=$3 WHERE account_id=$1 AND plan_key=$2;
-- name: RetireHistoryPartialJob :exec
UPDATE jobs SET state='done',claim_token='',lease_until=NULL WHERE id=$1;
-- name: ExpireHistoryPrompt :exec
UPDATE telegram_observations SET expires_at=$5 WHERE account_id=$1 AND scope=$2 AND chat_id=$3 AND message_id=$4 AND kind='log' AND NOT deleted;
-- name: FailHistoryJob :exec
UPDATE history_runs h SET state='failed',payload=jsonb_set(h.payload,'{State}','"failed"')
FROM jobs j WHERE j.id=$1 AND j.account_id=h.account_id AND j.payload->>'run'=h.run_id
AND (j.kind='history' OR j.kind IN ('history-message','history-finish')) AND j.state IN ('failed','uncertain') AND h.state NOT IN ('complete','failed');
-- name: ReconcileHistoryFailures :exec
UPDATE history_runs h SET state='failed',payload=jsonb_set(h.payload,'{State}','"failed"')
WHERE h.state NOT IN ('complete','failed') AND EXISTS(SELECT 1 FROM jobs j WHERE j.account_id=h.account_id AND j.payload->>'run'=h.run_id
AND (j.kind='history' OR j.kind IN ('history-message','history-finish')) AND j.state IN ('failed','uncertain'));

-- name: SyncHistoryActivity :exec
UPDATE history_activity a SET status=h.state FROM history_runs h WHERE h.account_id=a.account_id AND h.run_id=a.run_id;
-- name: HistoryDeliveryPlan :one
SELECT job_id,complete,whatsapp_chat,payload FROM relay_plans WHERE account_id=$1 AND plan_key=$2;
