-- name: LegacyCutoverPolicy :one
SELECT archive_digest,queue_mode FROM legacy_cutover_policies WHERE account_id=$1;
-- name: SaveLegacyCutoverPolicy :exec
INSERT INTO legacy_cutover_policies(account_id,archive_digest,queue_mode) VALUES($1,$2,'fresh');

-- name: SaveLegacyIngressDedup :exec
INSERT INTO legacy_ingress_dedup(account_id,dedup)
SELECT r.account_id,convert_from(r.raw_row,'UTF8')::jsonb->>'dedup' FROM legacy_rows r
WHERE r.account_id=$1 AND r.source_table='jobs';

-- name: LegacyIngressOverlap :one
SELECT EXISTS(SELECT 1 FROM jobs j JOIN legacy_rows r ON r.account_id=j.account_id AND r.source_table='jobs'
WHERE j.account_id=$1 AND j.dedup=convert_from(r.raw_row,'UTF8')::jsonb->>'dedup');
