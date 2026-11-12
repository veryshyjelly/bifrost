-- name: LegacyChangeProjection :one
SELECT result FROM legacy_change_projections WHERE account_id=$1;

-- name: SaveLegacyChangeProjection :exec
INSERT INTO legacy_change_projections(account_id,result) VALUES($1,$2);

-- name: LegacyChangeValues :many
SELECT kind,source_id,source_value FROM legacy_values WHERE account_id=$1 AND kind IN ('latest-edit','outbound-edit','revoked') ORDER BY kind,source_id;

-- name: LegacyChangeConflict :one
SELECT EXISTS(SELECT 1 FROM retained_message_edits r WHERE r.account_id=$1
 UNION ALL SELECT 1 FROM message_latest_edits e WHERE e.account_id=$1
 UNION ALL SELECT 1 FROM message_outbound_edits o WHERE o.account_id=$1
 UNION ALL SELECT 1 FROM message_tombstones t WHERE t.account_id=$1) AS conflict;

-- name: SaveRetainedMessageEdit :exec
INSERT INTO retained_message_edits(account_id,whatsapp_chat,message_id,source_key,body,mentioned_ids) VALUES($1,$2,$3,$4,$5,$6);
