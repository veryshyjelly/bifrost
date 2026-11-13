-- name: TryRuntimeLease :one
SELECT pg_try_advisory_lock(hashtext('bifrost-runtime'),hashtext(sqlc.arg(account_id)::text));
-- name: ReleaseRuntimeLease :one
SELECT pg_advisory_unlock(hashtext('bifrost-runtime'),hashtext(sqlc.arg(account_id)::text));

-- name: RuntimeBotReady :one
SELECT EXISTS(SELECT 1 FROM telegram_credentials c JOIN telegram_bot_lifecycle l ON l.account_id=c.account_id AND l.bot_id=c.bot_id
WHERE c.account_id=sqlc.arg(account_id) AND c.bot_id=sqlc.arg(bot_id)
AND EXISTS(SELECT 1 FROM telegram_setup_plans p WHERE p.account_id=c.account_id AND p.payload->>'BotID'=c.bot_id
 AND p.payload->>'Revision'=c.revision AND p.payload->>'Configuration'=sqlc.arg(configuration)::text
 AND (p.payload->>'Step')::integer>=sqlc.arg(minimum_step)::integer));

-- name: RuntimeBotsReady :one
SELECT EXISTS(SELECT 1 FROM telegram_credentials c WHERE c.account_id=sqlc.arg(account_id))
AND NOT EXISTS(SELECT 1 FROM telegram_credentials c WHERE c.account_id=sqlc.arg(account_id)
 AND NOT EXISTS(SELECT 1 FROM telegram_setup_plans p JOIN telegram_bot_lifecycle l ON l.account_id=p.account_id AND l.bot_id=c.bot_id
 WHERE p.account_id=c.account_id AND p.payload->>'BotID'=c.bot_id AND p.payload->>'Revision'=c.revision
 AND p.payload->>'Configuration'=sqlc.arg(configuration)::text AND (p.payload->>'Step')::integer>=3)) AS ready;

-- name: RuntimeBotSetupNeedsReview :one
SELECT EXISTS(SELECT 1 FROM jobs WHERE account_id=$1 AND kind='control-bot-setup' AND state IN ('failed','uncertain'));
