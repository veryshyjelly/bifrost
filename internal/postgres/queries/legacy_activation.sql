-- name: LegacyProjectionComplete :one
SELECT EXISTS(SELECT 1 FROM legacy_bot_projections x WHERE x.account_id=$1)
AND EXISTS(SELECT 1 FROM legacy_directory_projections x WHERE x.account_id=$1)
AND EXISTS(SELECT 1 FROM legacy_route_projections x WHERE x.account_id=$1)
AND EXISTS(SELECT 1 FROM legacy_completion_projections x WHERE x.account_id=$1)
AND EXISTS(SELECT 1 FROM legacy_message_projections x WHERE x.account_id=$1)
AND EXISTS(SELECT 1 FROM legacy_source_projections x WHERE x.account_id=$1)
AND EXISTS(SELECT 1 FROM legacy_change_projections x WHERE x.account_id=$1)
AND EXISTS(SELECT 1 FROM legacy_interaction_projections x WHERE x.account_id=$1)
AND EXISTS(SELECT 1 FROM legacy_incoming_reaction_projections x WHERE x.account_id=$1)
AND EXISTS(SELECT 1 FROM legacy_status_projections x WHERE x.account_id=$1) AS complete;

-- name: LegacyNativeQueueEmpty :one
SELECT NOT EXISTS(SELECT 1 FROM jobs x WHERE x.account_id=$1) AND NOT EXISTS(SELECT 1 FROM effects x WHERE x.account_id=$1) AS empty;

-- name: LegacyAssetVerification :one
SELECT archive_digest,receipt FROM legacy_asset_verifications x WHERE x.account_id=$1;

-- name: SaveLegacyAssetVerification :exec
INSERT INTO legacy_asset_verifications(account_id,archive_digest,receipt) VALUES($1,$2,$3);

-- name: ActivateLegacyImport :execrows
UPDATE legacy_imports SET activated_at=now() WHERE account_id=$1 AND archive_digest=$2 AND activated_at IS NULL;

-- name: RuntimeLegacyImport :one
SELECT * FROM legacy_imports x WHERE x.account_id=$1;
