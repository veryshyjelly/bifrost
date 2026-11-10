-- name: ConversationRoute :one
SELECT whatsapp_chat, name, telegram_chat_id, thread_id, mode, inbox_retired
FROM bridge_routes WHERE account_id = $1 AND whatsapp_chat = $2;

-- name: ContactBot :one
SELECT b.bot_id, b.started FROM bridge_contact_bots c
JOIN bridge_bots b ON b.account_id = c.account_id AND b.bot_id = c.bot_id
WHERE c.account_id = $1 AND c.contact_id = $2;

-- name: CreateInbox :one
INSERT INTO bridge_routes(account_id,whatsapp_chat,name,telegram_chat_id,thread_id,mode,inbox_retired)
VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING
RETURNING whatsapp_chat, name, telegram_chat_id, thread_id, mode, inbox_retired;

-- name: BindContactBot :exec
INSERT INTO bridge_contact_bots(account_id,contact_id,bot_id) VALUES($1,$2,$3)
ON CONFLICT(account_id,contact_id) DO UPDATE SET bot_id = EXCLUDED.bot_id;

-- name: SetBotStarted :execrows
UPDATE bridge_bots SET started = true WHERE account_id = $1 AND bot_id = $2;

-- name: ContactForBot :one
SELECT contact_id FROM bridge_contact_bots WHERE account_id = $1 AND bot_id = $2;

-- name: BindDirectRouteMode :exec
UPDATE bridge_routes SET mode = 'dedicated'
WHERE account_id = $1 AND whatsapp_chat = $2 AND mode IN ('inbox','dedicated');
