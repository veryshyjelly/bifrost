-- name: BotLifecycle :one
SELECT identity,migrated_at,menu_installed_at FROM telegram_bot_lifecycle WHERE account_id=$1 AND bot_id=$2;
-- name: SaveBotMigration :exec
INSERT INTO telegram_bot_lifecycle(account_id,bot_id,identity,migrated_at) VALUES($1,$2,$3,$4)
ON CONFLICT(account_id,bot_id) DO UPDATE SET identity=EXCLUDED.identity,migrated_at=EXCLUDED.migrated_at;
-- name: SaveTelegramIdentity :exec
UPDATE telegram_bot_lifecycle SET identity=$3 WHERE account_id=$1 AND bot_id=$2;
-- name: MigratedBots :many
SELECT l.bot_id FROM telegram_bot_lifecycle l WHERE l.account_id=$1 AND (l.bot_id='master' OR EXISTS(SELECT 1 FROM bridge_bots b WHERE b.account_id=l.account_id AND b.bot_id=l.bot_id)) ORDER BY l.bot_id;
-- name: MenuPlan :one
SELECT payload FROM telegram_menu_plans WHERE account_id=$1 AND job_id=$2;
-- name: PrepareMenu :exec
INSERT INTO telegram_menu_plans(account_id,job_id,payload) VALUES($1,$2,$3);
-- name: UpdateMenu :exec
UPDATE telegram_menu_plans SET payload=$3 WHERE account_id=$1 AND job_id=$2;
-- name: MarkMenuInstalled :exec
UPDATE telegram_bot_lifecycle SET menu_installed_at=$3 WHERE account_id=$1 AND bot_id=$2;
