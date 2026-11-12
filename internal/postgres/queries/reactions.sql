-- name: ReactionState :one
SELECT payload FROM message_reactions WHERE account_id=$1 AND whatsapp_chat=$2 AND message_id=$3;
-- name: SaveReactionState :exec
INSERT INTO message_reactions(account_id,whatsapp_chat,message_id,payload) VALUES($1,$2,$3,$4)
ON CONFLICT(account_id,whatsapp_chat,message_id) DO UPDATE SET payload=EXCLUDED.payload;
-- name: ReactionCache :one
SELECT payload FROM reaction_destinations WHERE account_id=$1 AND whatsapp_chat=$2 AND message_id=$3 AND destination_key=$4;
-- name: SaveReactionCache :exec
INSERT INTO reaction_destinations(account_id,whatsapp_chat,message_id,destination_key,payload) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(account_id,whatsapp_chat,message_id,destination_key) DO UPDATE SET payload=EXCLUDED.payload;
-- name: ReactionPlan :one
SELECT * FROM reaction_plans WHERE account_id=$1 AND plan_key=$2;
-- name: PrepareReactionPlan :exec
INSERT INTO reaction_plans(account_id,plan_key,job_id,payload) VALUES($1,$2,$3,$4);
-- name: UpdateReactionPlan :exec
UPDATE reaction_plans SET payload=$3 WHERE account_id=$1 AND plan_key=$2;
-- name: CompleteReactionPlan :exec
UPDATE reaction_plans SET complete=true WHERE account_id=$1 AND plan_key=$2;
-- name: OutgoingReactionPlan :one
SELECT * FROM outgoing_reaction_plans WHERE account_id=$1 AND plan_key=$2;
-- name: PrepareOutgoingReactionPlan :exec
INSERT INTO outgoing_reaction_plans(account_id,plan_key,job_id,payload) VALUES($1,$2,$3,$4);
-- name: UpdateOutgoingReactionPlan :exec
UPDATE outgoing_reaction_plans SET payload=$3 WHERE account_id=$1 AND plan_key=$2;
-- name: ReactionEcho :one
SELECT payload FROM reaction_echoes WHERE account_id=$1 AND whatsapp_chat=$2 AND message_id=$3;
-- name: SaveReactionEcho :exec
INSERT INTO reaction_echoes(account_id,whatsapp_chat,message_id,payload) VALUES($1,$2,$3,$4)
ON CONFLICT(account_id,whatsapp_chat,message_id) DO UPDATE SET payload=EXCLUDED.payload;
-- name: CustomReactionEmoji :one
SELECT emoji FROM custom_reaction_emojis WHERE account_id=$1 AND custom_id=$2;
-- name: SaveCustomReactionEmoji :exec
INSERT INTO custom_reaction_emojis(account_id,custom_id,emoji) VALUES($1,$2,$3)
ON CONFLICT(account_id,custom_id) DO UPDATE SET emoji=EXCLUDED.emoji;
