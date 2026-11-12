-- name: NativeDeliveryPlan :one
SELECT * FROM relay_plans WHERE account_id=$1 AND plan_key=$2;
-- name: UpdateNativePlan :exec
UPDATE relay_plans SET payload=$3 WHERE account_id=$1 AND plan_key=$2;
-- name: ArchivedAsset :one
SELECT asset FROM message_assets WHERE account_id=$1 AND whatsapp_chat=$2 AND message_id=$3;
-- name: SaveArchivedAsset :exec
INSERT INTO message_assets(account_id,whatsapp_chat,message_id,asset) VALUES($1,$2,$3,$4)
ON CONFLICT(account_id,whatsapp_chat,message_id) DO UPDATE SET asset=EXCLUDED.asset;
-- name: ArchivedMedia :many
SELECT m.bot_id,m.telegram_chat_id,m.telegram_message_id,m.receipt,
COALESCE(CASE WHEN j.payload->>'bot'=m.bot_id AND jsonb_typeof(j.payload->'update'->'message')='object' THEN j.payload END,m.source_payload) AS source FROM message_mappings m
LEFT JOIN relay_plans p ON p.account_id=m.account_id AND p.plan_key=m.plan_key LEFT JOIN jobs j ON j.id=p.job_id
WHERE m.account_id=$1 AND m.whatsapp_chat=$2 AND m.whatsapp_message_id=$3 ORDER BY m.seq;
-- name: HistoryQuote :one
SELECT m.* FROM history_items h JOIN message_mappings m ON m.account_id=h.account_id AND m.plan_key=h.plan_key
LEFT JOIN telegram_observations o ON o.account_id=m.account_id AND o.scope=m.scope AND o.chat_id=m.telegram_chat_id AND o.message_id=m.telegram_message_id
WHERE h.account_id=sqlc.arg(account_id) AND h.run_id=sqlc.arg(run_id) AND h.source_id=sqlc.arg(source_id) AND h.state='done'
AND m.telegram_chat_id=sqlc.arg(telegram_chat_id)::bigint AND m.thread_id=sqlc.arg(thread_id)::bigint AND m.scope=sqlc.arg(scope)::text
AND NOT m.deleted AND NOT COALESCE(o.deleted,false) AND COALESCE(o.thread_id,m.thread_id)=sqlc.arg(thread_id)::bigint
ORDER BY m.seq DESC LIMIT 1;
-- name: RecordHistoryDay :exec
UPDATE history_runs SET payload=jsonb_set(payload,'{LastDay}',to_jsonb(sqlc.arg(day)::text)) WHERE account_id=sqlc.arg(account_id) AND run_id=sqlc.arg(run_id);
-- name: CreateNativePollAction :exec
INSERT INTO native_poll_actions(account_id,action_id,plan_key,payload) VALUES($1,$2,$3,$4);
