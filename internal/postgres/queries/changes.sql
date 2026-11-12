-- name: ChangePlan :one
SELECT * FROM message_change_plans WHERE account_id=$1 AND plan_key=$2;
-- name: PrepareChange :exec
INSERT INTO message_change_plans(account_id,plan_key,job_id,payload) VALUES($1,$2,$3,$4);
-- name: UpdateChange :exec
UPDATE message_change_plans SET payload=$3 WHERE account_id=$1 AND plan_key=$2;
-- name: CompleteChange :exec
UPDATE message_change_plans SET complete=true WHERE account_id=$1 AND plan_key=$2;
-- name: LatestMessageEdit :one
SELECT n.account_id,n.whatsapp_chat,n.message_id,n.body,COALESCE(n.mentioned_ids,r.mentioned_ids) AS mentioned_ids,n.job_id
FROM message_latest_edits n LEFT JOIN retained_message_edits r USING(account_id,whatsapp_chat,message_id)
WHERE n.account_id=$1 AND n.whatsapp_chat=$2 AND n.message_id=$3
UNION ALL
SELECT r.account_id,r.whatsapp_chat,r.message_id,r.body,r.mentioned_ids,0::bigint AS job_id
FROM retained_message_edits r WHERE r.account_id=$1 AND r.whatsapp_chat=$2 AND r.message_id=$3
AND NOT EXISTS(SELECT 1 FROM message_latest_edits n WHERE n.account_id=r.account_id AND n.whatsapp_chat=r.whatsapp_chat AND n.message_id=r.message_id);
-- name: SaveLatestMessageEdit :exec
INSERT INTO message_latest_edits(account_id,whatsapp_chat,message_id,body,mentioned_ids,job_id) VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(account_id,whatsapp_chat,message_id) DO UPDATE SET body=EXCLUDED.body,mentioned_ids=COALESCE(EXCLUDED.mentioned_ids,message_latest_edits.mentioned_ids),job_id=EXCLUDED.job_id WHERE message_latest_edits.job_id<=EXCLUDED.job_id;
-- name: OutboundEditEcho :one
SELECT EXISTS(SELECT 1 FROM message_outbound_edits WHERE account_id=$1 AND whatsapp_chat=$2 AND message_id=$3 AND body_hash=$4);
-- name: SaveOutboundEdit :exec
INSERT INTO message_outbound_edits(account_id,whatsapp_chat,message_id,body_hash) VALUES($1,$2,$3,$4)
ON CONFLICT(account_id,whatsapp_chat,message_id) DO UPDATE SET body_hash=EXCLUDED.body_hash;
-- name: RetainedSourceMappings :many
SELECT m.*,p.payload AS plan_payload FROM message_mappings m
LEFT JOIN telegram_observations o ON o.account_id=m.account_id AND o.scope=m.scope AND o.chat_id=m.telegram_chat_id AND o.message_id=m.telegram_message_id
LEFT JOIN relay_plans p ON p.account_id=m.account_id AND p.plan_key=m.plan_key
WHERE m.account_id=$1 AND m.whatsapp_chat=$2 AND m.whatsapp_message_id=$3
AND NOT m.deleted AND NOT COALESCE(o.deleted,false) AND COALESCE(o.thread_id,m.thread_id)=m.thread_id
AND NOT EXISTS(SELECT 1 FROM retired_topics t WHERE t.account_id=m.account_id AND t.telegram_chat_id=m.telegram_chat_id AND t.thread_id=m.thread_id)
ORDER BY m.seq;
