-- name: LegacyDirectoryProjection :one
SELECT result FROM legacy_directory_projections WHERE account_id=$1;

-- name: SaveLegacyDirectoryProjection :exec
INSERT INTO legacy_directory_projections(account_id,result) VALUES($1,$2);

-- name: LegacyDirectoryValues :many
SELECT kind,source_id,source_value FROM legacy_values
WHERE account_id=$1 AND kind IN ('contact','mention-token','sync-group') ORDER BY kind,source_id;

-- name: LegacySourceAliases :many
SELECT (source.v->>'alias')::text AS alias,(source.v->>'canonical')::text AS canonical
FROM (SELECT convert_from(r.raw_row,'UTF8')::jsonb AS v FROM legacy_rows r
 WHERE r.account_id=$1 AND r.source_table='aliases') source ORDER BY alias;

-- name: LegacyDirectoryProjectionConflict :one
SELECT EXISTS(SELECT 1 FROM contact_aliases a WHERE a.account_id=$1
 UNION ALL SELECT 1 FROM contact_profiles p WHERE p.account_id=$1
 UNION ALL SELECT 1 FROM bridge_contact_bots b WHERE b.account_id=$1
 UNION ALL SELECT 1 FROM mention_rosters r WHERE r.account_id=$1
 UNION ALL SELECT 1 FROM mention_tokens t WHERE t.account_id=$1);

-- name: SaveLegacyContactBinding :exec
INSERT INTO bridge_contact_bots(account_id,contact_id,bot_id,source_profile) VALUES($1,$2,$3,$4);
