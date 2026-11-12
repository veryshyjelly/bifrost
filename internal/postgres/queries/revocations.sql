-- name: SetMessageRevoked :exec
INSERT INTO message_tombstones(account_id,whatsapp_chat,message_id,revoked) VALUES($1,$2,$3,true)
ON CONFLICT(account_id,whatsapp_chat,message_id) DO UPDATE SET revoked=true;
-- name: SourceMessageIsOwn :one
SELECT EXISTS(SELECT 1 FROM message_mappings m WHERE m.account_id=sqlc.arg(account_id) AND m.whatsapp_chat=sqlc.arg(whatsapp_chat) AND m.whatsapp_message_id=sqlc.arg(message_id) AND m.direction='out')
OR EXISTS(SELECT 1 FROM whatsapp_sources s WHERE s.account_id=sqlc.arg(account_id) AND s.whatsapp_chat=sqlc.arg(whatsapp_chat) AND s.payload->>'MessageID'=sqlc.arg(message_id) AND s.payload->>'Own'='true')
OR EXISTS(SELECT 1 FROM history_items i JOIN history_runs r ON r.account_id=i.account_id AND r.run_id=i.run_id WHERE i.account_id=sqlc.arg(account_id) AND r.whatsapp_chat=sqlc.arg(whatsapp_chat) AND i.source_id=sqlc.arg(message_id) AND i.payload->'Source'->>'Own'='true') AS own;
-- name: RevocationTask :one
SELECT * FROM revocation_tasks WHERE account_id=$1 AND task_id=$2;
-- name: CreateRevocationTask :exec
INSERT INTO revocation_tasks(account_id,task_id,whatsapp_chat,message_id,payload) VALUES($1,$2,$3,$4,$5);
-- name: SaveRevocationTask :exec
UPDATE revocation_tasks SET payload=$3 WHERE account_id=$1 AND task_id=$2;
-- name: QueueRevocationJob :exec
INSERT INTO jobs(account_id,dedup,stream,kind,payload,due) VALUES($1,$2,$3,'control-revoke',$4,to_timestamp(sqlc.arg(due_ms)::double precision/1000))
ON CONFLICT(account_id,dedup) DO NOTHING;
-- name: MarkRevokedCopy :exec
UPDATE message_mappings SET deleted=true WHERE account_id=$1 AND scope=$2 AND telegram_chat_id=$3 AND telegram_message_id=$4;
-- name: MarkRevokedObservation :exec
UPDATE telegram_observations SET deleted=true,cleanup_error='' WHERE account_id=$1 AND scope=$2 AND chat_id=$3 AND message_id=$4;
