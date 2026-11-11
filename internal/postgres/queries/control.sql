-- name: ControlPlan :one
SELECT job_id,payload,complete FROM control_plans WHERE account_id=$1 AND plan_key=$2;

-- name: PrepareControl :exec
INSERT INTO control_plans(account_id,plan_key,job_id,payload) VALUES($1,$2,$3,$4);

-- name: ControlAction :one
SELECT payload FROM control_actions WHERE account_id=$1 AND action_id=$2;

-- name: ControlWasCommand :one
SELECT EXISTS(SELECT 1 FROM telegram_observations WHERE account_id=$1 AND scope=$2 AND chat_id=$3 AND message_id=$4 AND kind='command');

-- name: CreateControlAction :exec
INSERT INTO control_actions(account_id,action_id,plan_key,payload) VALUES($1,$2,$3,$4);

-- name: LockControlRoute :one
SELECT * FROM bridge_routes WHERE account_id=$1 AND whatsapp_chat=$2 FOR UPDATE;

-- name: CompleteControl :exec
UPDATE control_plans SET complete=true WHERE account_id=$1 AND plan_key=$2;

-- name: SetConversationMode :exec
UPDATE bridge_routes SET mode=$3 WHERE account_id=$1 AND whatsapp_chat=$2;

-- name: ObserveControl :exec
INSERT INTO telegram_observations(account_id,scope,bot_id,chat_id,message_id,thread_id,kind,message_date,payload,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT(account_id,scope,chat_id,message_id) DO UPDATE SET
expires_at=CASE WHEN NOT telegram_observations.deleted AND telegram_observations.kind IN ('command','log') AND telegram_observations.expires_at=0 THEN EXCLUDED.expires_at ELSE telegram_observations.expires_at END;
