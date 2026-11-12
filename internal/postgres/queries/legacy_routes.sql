-- name: LegacyRouteProjection :one
SELECT result FROM legacy_route_projections WHERE account_id=$1;

-- name: SaveLegacyRouteProjection :exec
INSERT INTO legacy_route_projections(account_id,result) VALUES($1,$2);

-- name: LegacyRouteValues :many
SELECT kind,source_id,source_value FROM legacy_values
WHERE account_id=$1 AND kind IN ('route','retired-topic') ORDER BY kind,source_id;

-- name: LegacyRouteProjectionConflict :one
SELECT EXISTS(SELECT 1 FROM bridge_routes r WHERE r.account_id=$1
 UNION ALL SELECT 1 FROM conversation_inboxes i WHERE i.account_id=$1
 UNION ALL SELECT 1 FROM retired_topics t WHERE t.account_id=$1
 UNION ALL SELECT 1 FROM mirror_returns m WHERE m.account_id=$1
 UNION ALL SELECT 1 FROM metadata_topic_names n WHERE n.account_id=$1);

-- name: LegacyTopicCoordinates :many
WITH source AS (
 SELECT r.source_table,convert_from(r.raw_row,'UTF8')::jsonb AS v FROM legacy_rows r
 WHERE r.account_id=sqlc.arg(account_id) AND r.source_table IN ('effects','telegram_messages')
), receipts AS (
 SELECT (s.v->>'result')::jsonb AS receipt FROM source s
 WHERE s.source_table='effects' AND s.v->>'state'='done' AND s.v->>'result' IS NOT NULL
 UNION ALL SELECT l.source_value::jsonb FROM legacy_values l
 WHERE l.account_id=sqlc.arg(account_id) AND l.kind='telegram-receipt'
), coordinates AS (
 SELECT COALESCE(r.receipt->'chat'->>'id',sqlc.arg(management)::text)::text AS chat,
 (r.receipt->>'message_thread_id')::text AS thread FROM receipts r
 WHERE r.receipt->>'message_thread_id' IS NOT NULL
 UNION SELECT (s.v->>'chat')::text,(s.v->>'thread')::text FROM source s WHERE s.source_table='telegram_messages'
 UNION SELECT (l.source_value::jsonb->>'destination')::text,(l.source_value::jsonb->>'thread')::text FROM legacy_values l
 WHERE l.account_id=sqlc.arg(account_id) AND l.kind='delivery-target'
 UNION SELECT (l.source_value::jsonb->>'tgChat')::text,(l.source_value::jsonb->>'thread')::text FROM legacy_values l
 WHERE l.account_id=sqlc.arg(account_id) AND l.kind='status-feed'
)
SELECT DISTINCT c.chat::text AS chat,c.thread::text AS thread FROM coordinates c WHERE c.chat IS NOT NULL AND c.thread IS NOT NULL ORDER BY c.chat,c.thread;
