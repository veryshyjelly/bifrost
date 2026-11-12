-- name: LegacySourceProjection :one
SELECT result FROM legacy_source_projections WHERE account_id=$1;

-- name: SaveLegacySourceProjection :exec
INSERT INTO legacy_source_projections(account_id,result) VALUES($1,$2);

-- name: LegacySourceValues :many
SELECT kind,source_id,source_value FROM legacy_values WHERE account_id=$1 AND kind='wamessage' ORDER BY source_id;

-- name: RetainedSourceConflict :one
SELECT EXISTS(SELECT 1 FROM retained_message_sources WHERE account_id=$1);

-- name: SaveRetainedMessageSource :exec
INSERT INTO retained_message_sources(account_id,whatsapp_chat,message_id,source_key,payload) VALUES($1,$2,$3,$4,$5);

-- name: SourceActorIsOwn :one
SELECT EXISTS(SELECT 1 FROM whatsapp_sources w WHERE w.account_id=sqlc.arg(account_id)
 AND (w.whatsapp_chat LIKE '%@g.us' OR w.whatsapp_chat='status@broadcast')
 AND w.payload->>'Own'='true' AND w.payload->>'SenderID'=ANY(sqlc.arg(identities)::text[]))
OR EXISTS(SELECT 1 FROM retained_message_sources s WHERE s.account_id=sqlc.arg(account_id)
 AND (s.whatsapp_chat LIKE '%@g.us' OR s.whatsapp_chat='status@broadcast')
 AND s.payload->>'Own'='true' AND s.payload->>'SenderID'=ANY(sqlc.arg(identities)::text[]))
OR EXISTS(SELECT 1 FROM history_items i JOIN history_runs r ON r.account_id=i.account_id AND r.run_id=i.run_id
 WHERE i.account_id=sqlc.arg(account_id) AND r.whatsapp_chat LIKE '%@g.us'
 AND i.payload->'Source'->>'Own'='true' AND i.payload->'Source'->>'SenderID'=ANY(sqlc.arg(identities)::text[]));
