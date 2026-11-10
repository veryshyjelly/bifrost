-- name: LockQueueTopology :exec
SELECT pg_advisory_xact_lock(725104322);

-- name: WhatsAppSource :one
SELECT whatsapp_chat,payload FROM whatsapp_sources WHERE account_id=$1 AND job_id=$2;

-- name: SaveWhatsAppSource :exec
INSERT INTO whatsapp_sources(account_id,job_id,whatsapp_chat,payload) VALUES($1,$2,$3,$4);

-- name: ExistingConversations :many
SELECT r.whatsapp_chat FROM bridge_routes r WHERE r.account_id=sqlc.arg(account_id) AND r.whatsapp_chat=ANY(sqlc.arg(identities)::text[])
UNION SELECT r.whatsapp_chat FROM relay_plans r WHERE r.account_id=sqlc.arg(account_id) AND r.whatsapp_chat=ANY(sqlc.arg(identities)::text[])
UNION SELECT r.whatsapp_chat FROM message_mappings r WHERE r.account_id=sqlc.arg(account_id) AND r.whatsapp_chat=ANY(sqlc.arg(identities)::text[])
UNION SELECT r.whatsapp_chat FROM whatsapp_sources r WHERE r.account_id=sqlc.arg(account_id) AND r.whatsapp_chat=ANY(sqlc.arg(identities)::text[]);

-- name: JobEffectCount :one
SELECT count(*) FROM effects WHERE job_id=$1;

-- name: ConversationBusy :one
SELECT EXISTS(SELECT 1 FROM jobs WHERE account_id=sqlc.arg(account_id) AND stream=sqlc.arg(stream)
 AND id<>sqlc.arg(job_id) AND state<>'done' AND (id<sqlc.arg(job_id) OR state='running'));

-- name: RestreamJob :exec
UPDATE jobs SET stream=$2 WHERE id=$1;

-- name: ParkRestreamedJob :exec
UPDATE jobs SET stream=$2,state='pending',claim_token='',lease_until=NULL,due=clock_timestamp(),attempts=GREATEST(attempts-1,0) WHERE id=$1;

-- name: SelfIdentity :one
SELECT person_id FROM whatsapp_self WHERE account_id=$1;

-- name: SaveSelfIdentity :one
INSERT INTO whatsapp_self(account_id,person_id) VALUES($1,$2)
ON CONFLICT(account_id) DO UPDATE SET account_id=EXCLUDED.account_id RETURNING person_id;
