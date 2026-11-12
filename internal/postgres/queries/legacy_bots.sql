-- name: LockLegacyImport :one
SELECT * FROM legacy_imports WHERE account_id=$1 FOR UPDATE;

-- name: LegacyBotValues :many
SELECT kind,source_id,source_value FROM legacy_values
WHERE account_id=$1 AND kind IN ('bot','migrated','telegram-identity') ORDER BY kind,source_id;

-- name: LegacyBotProjection :one
SELECT result FROM legacy_bot_projections WHERE account_id=$1;

-- name: SaveLegacyBotProjection :exec
INSERT INTO legacy_bot_projections(account_id,result) VALUES($1,$2);

-- name: SaveLegacyBotLogout :exec
INSERT INTO legacy_bot_logouts(account_id,bot_id,credential_revision) VALUES($1,$2,$3);

-- name: LegacyBotLogout :one
SELECT EXISTS(SELECT 1 FROM legacy_bot_logouts WHERE account_id=$1 AND bot_id=$2 AND credential_revision=$3);

-- name: LegacyBotProjectionConflict :one
SELECT EXISTS(SELECT 1 FROM telegram_credentials c WHERE c.account_id=$1
 UNION ALL SELECT 1 FROM bridge_bots b WHERE b.account_id=$1
 UNION ALL SELECT 1 FROM telegram_bot_lifecycle l WHERE l.account_id=$1
 UNION ALL SELECT 1 FROM telegram_setup_plans p WHERE p.account_id=$1
 UNION ALL SELECT 1 FROM telegram_registrations r WHERE r.account_id=$1);
