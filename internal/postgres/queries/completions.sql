-- name: MessageComplete :one
SELECT EXISTS(SELECT 1 FROM source_completions WHERE account_id=$1 AND whatsapp_chat=$2 AND message_id=$3);

-- name: RecordSourceCompletion :exec
INSERT INTO source_completions(account_id,whatsapp_chat,message_id,source_key) VALUES($1,$2,$3,$4);

-- name: LegacyCompletionProjection :one
SELECT result FROM legacy_completion_projections WHERE account_id=$1;

-- name: SaveLegacyCompletionProjection :exec
INSERT INTO legacy_completion_projections(account_id,result) VALUES($1,$2);

-- name: LegacyCompletionValues :many
SELECT kind,source_id,source_value FROM legacy_values WHERE account_id=$1 AND kind='wa-complete' ORDER BY source_id;

-- name: LegacyCompletionConflict :one
SELECT EXISTS(SELECT 1 FROM source_completions WHERE account_id=$1);
