-- name: RelayPlan :one
SELECT payload, complete FROM relay_plans WHERE account_id=$1 AND plan_key=$2;

-- name: PrepareRelay :exec
INSERT INTO relay_plans(account_id,plan_key,job_id,direction,whatsapp_chat,payload)
VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING;

-- name: RelayEcho :one
SELECT EXISTS(SELECT 1 FROM message_mappings WHERE account_id=sqlc.arg(account_id)::text AND whatsapp_chat=sqlc.arg(whatsapp_chat)::text AND whatsapp_message_id=sqlc.arg(whatsapp_message_id)::text AND direction='out')
OR EXISTS(SELECT 1 FROM relay_plans p JOIN effects e ON e.account_id=p.account_id AND e.effect_key=p.payload->>'Prefix'
WHERE p.account_id=sqlc.arg(account_id)::text AND p.whatsapp_chat=sqlc.arg(whatsapp_chat)::text AND p.direction='out' AND e.state='done' AND e.result->>'messageId'=sqlc.arg(whatsapp_message_id)::text) AS recognized,
EXISTS(SELECT 1 FROM relay_plans p JOIN effects e ON e.account_id=p.account_id AND e.effect_key=p.payload->>'Prefix'
JOIN jobs j ON j.id=e.job_id
WHERE p.account_id=sqlc.arg(account_id)::text AND p.whatsapp_chat=sqlc.arg(whatsapp_chat)::text AND p.direction='out' AND e.state IN ('sending','uncertain') AND j.state <> 'done') AS pending;

-- name: WhatsAppQuote :one
SELECT m.* FROM message_mappings m LEFT JOIN telegram_observations o
ON o.account_id=m.account_id AND o.scope=m.scope AND o.chat_id=m.telegram_chat_id AND o.message_id=m.telegram_message_id
WHERE m.account_id=sqlc.arg(account_id) AND m.whatsapp_chat=sqlc.arg(whatsapp_chat) AND m.whatsapp_message_id=sqlc.arg(whatsapp_message_id)
AND m.scope=sqlc.arg(scope) AND m.telegram_chat_id=sqlc.arg(telegram_chat_id) AND m.thread_id=sqlc.arg(thread_id)
AND NOT m.deleted AND NOT COALESCE(o.deleted,false) AND COALESCE(o.thread_id,m.thread_id)=sqlc.arg(thread_id)
AND NOT EXISTS(SELECT 1 FROM retired_topics r WHERE r.account_id=m.account_id AND r.telegram_chat_id=m.telegram_chat_id AND r.thread_id=m.thread_id)
ORDER BY m.seq DESC LIMIT 1;

-- name: TelegramMapping :one
SELECT m.*, (COALESCE(o.deleted,false) OR EXISTS(SELECT 1 FROM retired_topics r WHERE r.account_id=m.account_id AND r.telegram_chat_id=m.telegram_chat_id AND r.thread_id=m.thread_id))::boolean AS observation_deleted FROM message_mappings m LEFT JOIN telegram_observations o
ON o.account_id=m.account_id AND o.scope=m.scope AND o.chat_id=m.telegram_chat_id AND o.message_id=m.telegram_message_id
WHERE m.account_id=$1 AND m.scope=$2 AND m.telegram_chat_id=$3 AND m.telegram_message_id=$4
ORDER BY m.seq LIMIT 1;

-- name: InsertRelayMapping :exec
INSERT INTO message_mappings(account_id,scope,telegram_chat_id,telegram_message_id,thread_id,bot_id,whatsapp_chat,whatsapp_message_id,direction,plan_key,part,receipt)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT DO NOTHING;

-- name: CompleteRelay :exec
UPDATE relay_plans SET complete=true WHERE account_id=$1 AND plan_key=$2;

-- name: OutgoingRoute :one
SELECT whatsapp_chat,name,telegram_chat_id,thread_id,mode,inbox_retired FROM bridge_routes
WHERE account_id=sqlc.arg(account_id) AND telegram_chat_id=sqlc.arg(telegram_chat_id)::bigint
AND NOT (telegram_chat_id=sqlc.arg(management_chat_id)::bigint AND inbox_retired AND thread_id=0)
AND (thread_id=0 OR thread_id=sqlc.arg(thread_id)::bigint)
ORDER BY whatsapp_chat LIMIT 1;
