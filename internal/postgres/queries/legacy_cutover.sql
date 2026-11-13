-- name: LegacyCutoverPolicy :one
SELECT archive_digest,queue_mode FROM legacy_cutover_policies WHERE account_id=$1;
-- name: SaveLegacyCutoverPolicy :exec
INSERT INTO legacy_cutover_policies(account_id,archive_digest,queue_mode) VALUES($1,$2,'fresh');
