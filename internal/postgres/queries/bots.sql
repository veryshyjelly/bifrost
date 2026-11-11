-- name: BotCredential :one
SELECT * FROM telegram_credentials WHERE account_id=$1 AND bot_id=$2;
-- name: BotCredentials :many
SELECT * FROM telegram_credentials WHERE account_id=$1 ORDER BY bot_id;
-- name: SaveBotCredential :exec
INSERT INTO telegram_credentials(account_id,bot_id,ciphertext,revision,identity,created_notice) VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(account_id,bot_id) DO UPDATE SET ciphertext=EXCLUDED.ciphertext,revision=EXCLUDED.revision,identity=EXCLUDED.identity;
-- name: BotRegistration :one
SELECT bot_id,revision FROM telegram_registrations WHERE account_id=$1 AND job_id=$2;
-- name: SaveBotRegistration :exec
INSERT INTO telegram_registrations(account_id,job_id,bot_id,revision) VALUES($1,$2,$3,$4);
-- name: BotSetupPlan :one
SELECT payload FROM telegram_setup_plans WHERE account_id=$1 AND job_id=$2;
-- name: SaveBotSetupPlan :exec
INSERT INTO telegram_setup_plans(account_id,job_id,payload) VALUES($1,$2,$3)
ON CONFLICT(account_id,job_id) DO UPDATE SET payload=EXCLUDED.payload;
-- name: MarkBotCreatedNotice :exec
UPDATE telegram_credentials SET created_notice=true WHERE account_id=$1 AND bot_id=$2;
-- name: ConfirmedBotLogout :one
SELECT EXISTS(SELECT 1 FROM effects e JOIN jobs j ON j.id=e.job_id AND j.account_id=e.account_id
WHERE e.account_id=sqlc.arg(account_id) AND j.kind='control-bot-setup' AND j.payload->>'bot'=sqlc.arg(bot_id)::text
AND e.effect_key=j.dedup||':cloud-logout' AND e.state='done' AND e.result='true'::jsonb
AND e.request->>'Bot'=sqlc.arg(bot_id)::text AND e.request->>'Revision'=j.payload->>'revision');
