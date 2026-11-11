-- name: InboxRetirement :one
SELECT payload,complete FROM inbox_retirements WHERE account_id=$1 AND job_id=$2;

-- name: PrepareInboxRetirement :exec
INSERT INTO inbox_retirements(account_id,job_id,payload,complete) VALUES($1,$2,$3,$4);

-- name: CompleteInboxRetirement :exec
UPDATE inbox_retirements SET complete=true WHERE account_id=$1 AND job_id=$2;

-- name: RememberInbox :exec
INSERT INTO conversation_inboxes(account_id,whatsapp_chat,telegram_chat_id,thread_id)
VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING;

-- name: RetirementTopics :many
SELECT i.thread_id FROM conversation_inboxes i WHERE i.account_id=sqlc.arg(account_id)::text AND i.whatsapp_chat=sqlc.arg(whatsapp_chat)::text AND i.telegram_chat_id=sqlc.arg(management)::bigint
UNION SELECT b.thread_id FROM bridge_routes b WHERE b.account_id=sqlc.arg(account_id)::text AND b.whatsapp_chat=sqlc.arg(whatsapp_chat)::text AND b.telegram_chat_id=sqlc.arg(management)::bigint AND b.thread_id>1
UNION SELECT COALESCE(o.thread_id,m.thread_id) AS thread_id FROM message_mappings m LEFT JOIN telegram_observations o
ON o.account_id=m.account_id AND o.scope=m.scope AND o.chat_id=m.telegram_chat_id AND o.message_id=m.telegram_message_id
WHERE m.account_id=sqlc.arg(account_id)::text AND m.whatsapp_chat=sqlc.arg(whatsapp_chat)::text AND m.scope='*' AND m.telegram_chat_id=sqlc.arg(management)::bigint
AND COALESCE(o.thread_id,m.thread_id)>1 AND NOT COALESCE(o.deleted,m.deleted)
ORDER BY thread_id;

-- name: SharedRetirementTopic :one
SELECT (EXISTS(SELECT 1 FROM bridge_routes b WHERE b.account_id=sqlc.arg(account_id)::text AND b.whatsapp_chat<>sqlc.arg(whatsapp_chat)::text AND b.telegram_chat_id=sqlc.arg(management)::bigint AND b.thread_id=sqlc.arg(thread_id)::bigint)
OR EXISTS(SELECT 1 FROM message_mappings m LEFT JOIN telegram_observations o
ON o.account_id=m.account_id AND o.scope=m.scope AND o.chat_id=m.telegram_chat_id AND o.message_id=m.telegram_message_id
WHERE m.account_id=sqlc.arg(account_id)::text AND m.whatsapp_chat<>sqlc.arg(whatsapp_chat)::text AND m.scope='*' AND m.telegram_chat_id=sqlc.arg(management)::bigint
AND (m.thread_id=sqlc.arg(thread_id)::bigint OR o.thread_id=sqlc.arg(thread_id)::bigint))
OR EXISTS(SELECT 1 FROM relay_plans p WHERE p.account_id=sqlc.arg(account_id)::text AND p.whatsapp_chat<>sqlc.arg(whatsapp_chat)::text
AND (p.payload->>'TelegramChat')::bigint=sqlc.arg(management)::bigint AND (p.payload->>'ThreadID')::bigint=sqlc.arg(thread_id)::bigint))::boolean;

-- name: IncompleteTopicRelay :one
SELECT EXISTS(SELECT 1 FROM relay_plans WHERE account_id=$1 AND NOT complete
AND (payload->>'TelegramChat')::bigint=$2::bigint AND (payload->>'ThreadID')::bigint=$3::bigint);

-- name: ReservedTopic :one
SELECT EXISTS(SELECT 1 FROM retired_topics WHERE account_id=$1 AND telegram_chat_id=$2 AND thread_id=$3);

-- name: RetiredTopic :one
SELECT whatsapp_chat,deleted FROM retired_topics WHERE account_id=$1 AND telegram_chat_id=$2 AND thread_id=$3;

-- name: ReserveRetirementTopic :exec
INSERT INTO retired_topics(account_id,telegram_chat_id,thread_id,whatsapp_chat) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING;

-- name: DeleteRetirementTopic :exec
UPDATE retired_topics SET deleted=true WHERE account_id=$1 AND telegram_chat_id=$2 AND thread_id=$3;

-- name: MarkRetiredTopicObservations :exec
UPDATE telegram_observations SET deleted=true,cleanup_error='' WHERE account_id=$1 AND scope='*' AND chat_id=$2 AND thread_id=$3;

-- name: RetireCurrentInbox :exec
UPDATE bridge_routes SET thread_id=0,inbox_retired=true WHERE account_id=$1 AND whatsapp_chat=$2 AND telegram_chat_id=$3 AND thread_id=ANY($4::bigint[]);
