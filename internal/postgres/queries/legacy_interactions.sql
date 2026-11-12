-- name: LegacyInteractionProjection :one
SELECT result FROM legacy_interaction_projections WHERE account_id=$1;

-- name: SaveLegacyInteractionProjection :exec
INSERT INTO legacy_interaction_projections(account_id,result) VALUES($1,$2);

-- name: LegacyInteractionValues :many
SELECT kind,source_id,source_value FROM legacy_values WHERE account_id=$1 AND kind IN ('poll-selection','custom-reaction-emoji','outgoing-reaction') ORDER BY kind,source_id;

-- name: LegacyInteractionConflict :one
SELECT EXISTS(SELECT 1 FROM poll_selections p WHERE p.account_id=$1
 UNION ALL SELECT 1 FROM custom_reaction_emojis c WHERE c.account_id=$1
 UNION ALL SELECT 1 FROM retained_reaction_choices r WHERE r.account_id=$1
 UNION ALL SELECT 1 FROM outgoing_reaction_plans o WHERE o.account_id=$1);

-- name: RetainedReactionChoice :one
SELECT emoji,supported FROM retained_reaction_choices WHERE account_id=$1 AND source_key=$2;

-- name: SaveRetainedReactionChoice :exec
INSERT INTO retained_reaction_choices(account_id,source_key,emoji,supported) VALUES($1,$2,$3,$4);
