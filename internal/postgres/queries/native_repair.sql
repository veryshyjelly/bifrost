-- name: NativeSourceMapped :one
SELECT EXISTS(SELECT 1 FROM message_mappings WHERE account_id=$1 AND whatsapp_chat=$2 AND whatsapp_message_id=$3);
-- name: RestoreNativeInbox :exec
UPDATE bridge_routes SET thread_id=$3,inbox_retired=false WHERE account_id=$1 AND whatsapp_chat=$2;
-- name: RetargetNativePollAction :exec
UPDATE native_poll_actions SET payload=jsonb_set(payload,'{Target}',sqlc.arg(target)::jsonb) WHERE account_id=sqlc.arg(account_id) AND plan_key=sqlc.arg(plan_key);
