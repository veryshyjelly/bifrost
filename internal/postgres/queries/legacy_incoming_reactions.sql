-- name: LegacyIncomingReactionProjection :one
SELECT result FROM legacy_incoming_reaction_projections WHERE account_id=$1;

-- name: SaveLegacyIncomingReactionProjection :exec
INSERT INTO legacy_incoming_reaction_projections(account_id,result) VALUES($1,$2);

-- name: LegacyIncomingReactionValues :many
SELECT kind,source_id,source_value FROM legacy_values WHERE account_id=$1 AND kind IN
('wa-reaction-state','tg-reaction-state','reaction-echo','wamessage','latest-edit','revoked','wa-complete','poll-selection') ORDER BY kind,source_id;

-- name: LegacyIncomingReactionRows :many
SELECT source_table,raw_row FROM legacy_rows WHERE account_id=$1 AND source_table IN ('jobs','messages') ORDER BY ordinal;

-- name: LegacyIncomingReactionCopies :many
SELECT bot_id,whatsapp_chat,whatsapp_message_id,telegram_chat_id,telegram_message_id,thread_id FROM message_mappings WHERE account_id=$1 ORDER BY seq;

-- name: LegacyIncomingReactionConflict :one
SELECT EXISTS(SELECT 1 FROM message_reactions s WHERE s.account_id=$1
 UNION ALL SELECT 1 FROM reaction_destinations d WHERE d.account_id=$1
 UNION ALL SELECT 1 FROM reaction_echoes e WHERE e.account_id=$1
 UNION ALL SELECT 1 FROM reaction_plans p WHERE p.account_id=$1
 UNION ALL SELECT 1 FROM outgoing_reaction_plans o WHERE o.account_id=$1
 UNION ALL SELECT 1 FROM legacy_pending_reaction_notices n WHERE n.account_id=$1);

-- name: SaveLegacyPendingReactionNotice :exec
INSERT INTO legacy_pending_reaction_notices(account_id,source_key,whatsapp_chat,message_id,destination_key,legacy_job_key) VALUES($1,$2,$3,$4,$5,$6);
