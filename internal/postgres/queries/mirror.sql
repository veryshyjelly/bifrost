-- name: MirrorReturnKey :one
SELECT COALESCE((SELECT return_key FROM mirror_returns WHERE account_id=$1 AND whatsapp_chat=$2),'')::text;
-- name: SaveMirrorReturn :exec
INSERT INTO mirror_returns(account_id,whatsapp_chat,return_key) VALUES($1,$2,$3)
ON CONFLICT(account_id,whatsapp_chat) DO UPDATE SET return_key=EXCLUDED.return_key;
-- name: MirrorTransition :one
SELECT payload FROM mirror_transitions WHERE account_id=$1 AND job_id=$2;
-- name: CreateMirrorTransition :exec
INSERT INTO mirror_transitions(account_id,job_id,payload) VALUES($1,$2,$3);
-- name: SaveMirrorTransition :exec
UPDATE mirror_transitions SET payload=$3 WHERE account_id=$1 AND job_id=$2;
-- name: RouteTransitionBusy :one
SELECT EXISTS(SELECT 1 FROM route_transition_locks WHERE account_id=$1 AND whatsapp_chat=$2 AND job_id<>$3);
-- name: LockRouteTransition :exec
INSERT INTO route_transition_locks(account_id,whatsapp_chat,job_id) VALUES($1,$2,$3);
-- name: UnlockRouteTransition :exec
DELETE FROM route_transition_locks WHERE account_id=$1 AND whatsapp_chat=$2 AND job_id=$3;
-- name: ActiveHistory :one
SELECT EXISTS(SELECT 1 FROM history_activity WHERE account_id=$1 AND whatsapp_chat=$2 AND status IN ('queued','fetching','delivering'));
-- name: RecordHistoryActivity :execrows
INSERT INTO history_activity(account_id,whatsapp_chat,run_id,status) VALUES($1,$2,$3,$4)
ON CONFLICT(account_id,whatsapp_chat) DO UPDATE SET run_id=EXCLUDED.run_id,status=EXCLUDED.status
WHERE history_activity.run_id=EXCLUDED.run_id OR history_activity.status IN ('complete','failed','cancelled');
-- name: OtherDestinationRoute :one
SELECT EXISTS(SELECT 1 FROM bridge_routes WHERE account_id=$1 AND telegram_chat_id=$2 AND whatsapp_chat<>$3);
-- name: OtherTopicRoute :one
SELECT EXISTS(SELECT 1 FROM bridge_routes WHERE account_id=$1 AND telegram_chat_id=$2 AND thread_id=$3 AND whatsapp_chat<>$4);
-- name: MoveMirrorRoute :exec
UPDATE bridge_routes SET telegram_chat_id=$3,thread_id=$4,mode=$5,inbox_retired=false WHERE account_id=$1 AND whatsapp_chat=$2;
