-- name: IngressBot :one
SELECT bot_id, username FROM bridge_bots WHERE account_id = $1 AND bot_id = $2;

-- name: PutIngressBot :exec
INSERT INTO bridge_bots(account_id, bot_id, username) VALUES($1,$2,$3)
ON CONFLICT(account_id,bot_id) DO UPDATE SET username = EXCLUDED.username;

-- name: IngressRoute :one
SELECT whatsapp_chat, telegram_chat_id, thread_id, inbox_retired FROM bridge_routes
WHERE account_id = sqlc.arg(account_id) AND telegram_chat_id = sqlc.arg(telegram_chat_id)::bigint
AND NOT (telegram_chat_id = sqlc.arg(management_chat_id)::bigint AND inbox_retired AND thread_id = 0)
AND (thread_id = 0 OR thread_id = sqlc.arg(thread_id)::bigint)
ORDER BY whatsapp_chat LIMIT 1;

-- name: PutIngressRoute :exec
INSERT INTO bridge_routes(account_id,whatsapp_chat,telegram_chat_id,thread_id,inbox_retired)
VALUES($1,$2,$3,$4,$5)
ON CONFLICT(account_id,whatsapp_chat) DO UPDATE SET
telegram_chat_id = EXCLUDED.telegram_chat_id, thread_id = EXCLUDED.thread_id, inbox_retired = EXCLUDED.inbox_retired;

-- name: ObserveTelegram :exec
INSERT INTO telegram_observations(account_id,scope,bot_id,chat_id,message_id,thread_id,kind,message_date,payload)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(account_id,scope,chat_id,message_id) DO UPDATE SET
kind = CASE WHEN telegram_observations.kind IN ('chat','command') THEN telegram_observations.kind ELSE EXCLUDED.kind END;
