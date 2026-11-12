-- name: LegacyMessageProjection :one
SELECT result FROM legacy_message_projections WHERE account_id=$1;

-- name: SaveLegacyMessageProjection :exec
INSERT INTO legacy_message_projections(account_id,result) VALUES($1,$2);

-- name: LegacyMessageValues :many
SELECT kind,source_id,source_value FROM legacy_values WHERE account_id=$1
AND kind IN ('bot','delivery-target','telegram-receipt','message-presentation','wamessage','reaction-self') ORDER BY kind,source_id;

-- name: LegacyMessageRows :many
SELECT source_table,raw_row FROM legacy_rows WHERE account_id=$1
AND source_table IN ('jobs','effects','messages','telegram_messages') ORDER BY ordinal;

-- name: LegacyMessageConflict :one
SELECT EXISTS(SELECT 1 FROM message_mappings m WHERE m.account_id=$1
 UNION ALL SELECT 1 FROM telegram_observations o WHERE o.account_id=$1
 UNION ALL SELECT 1 FROM whatsapp_self s WHERE s.account_id=$1);

-- name: InsertLegacyMapping :exec
INSERT INTO message_mappings(seq,account_id,scope,telegram_chat_id,telegram_message_id,thread_id,bot_id,
whatsapp_chat,whatsapp_message_id,direction,receipt,media,presentation,mentioned_ids,source_payload,receipt_origin)
OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16);

-- name: InsertLegacyObservation :exec
INSERT INTO telegram_observations(seq,account_id,scope,bot_id,chat_id,message_id,thread_id,kind,message_date,payload,deleted,expires_at,cleanup_error)
OVERRIDING SYSTEM VALUE VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13);

-- name: RetiredObservationTopics :many
SELECT telegram_chat_id,thread_id FROM retired_topics WHERE account_id=$1 AND deleted ORDER BY telegram_chat_id,thread_id;
