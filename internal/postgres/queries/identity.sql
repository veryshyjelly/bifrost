-- name: IdentityAliases :many
SELECT alias,canonical FROM contact_aliases WHERE account_id=$1 ORDER BY alias;

-- name: IdentityProfiles :many
SELECT person_id,name,metadata_synced FROM contact_profiles WHERE account_id=$1 ORDER BY person_id;

-- name: IdentityBots :many
SELECT bot_id,username,started FROM bridge_bots WHERE account_id=$1 ORDER BY bot_id;

-- name: IdentityBindings :many
SELECT COALESCE(NULLIF(source_profile,''),contact_id)::text AS contact_id,bot_id
FROM bridge_contact_bots WHERE account_id=$1 ORDER BY contact_id;

-- name: SaveContactAlias :one
INSERT INTO contact_aliases(account_id,alias,canonical) VALUES($1,$2,$3)
ON CONFLICT(account_id,alias) DO UPDATE SET alias=EXCLUDED.alias
RETURNING canonical;

-- name: SaveContactProfile :exec
WITH saved AS (
INSERT INTO contact_profiles(account_id,person_id,name,metadata_synced)
VALUES(sqlc.arg(account_id),sqlc.arg(person_id),sqlc.arg(name),sqlc.arg(metadata_synced))
ON CONFLICT(account_id,person_id) DO UPDATE SET
name=CASE WHEN NOT contact_profiles.metadata_synced OR EXCLUDED.metadata_synced THEN EXCLUDED.name ELSE contact_profiles.name END,
metadata_synced=contact_profiles.metadata_synced OR EXCLUDED.metadata_synced
RETURNING account_id,person_id
)
UPDATE bridge_contact_bots b SET source_profile='' FROM saved p
WHERE b.account_id=p.account_id AND b.contact_id=p.person_id AND sqlc.arg(metadata_synced)::boolean;

-- name: MentionRoster :one
SELECT payload,fetched_at FROM mention_rosters WHERE account_id=$1 AND chat_id=$2;

-- name: SaveMentionRoster :exec
INSERT INTO mention_rosters(account_id,chat_id,payload,fetched_at) VALUES($1,$2,$3,$4)
ON CONFLICT(account_id,chat_id) DO UPDATE SET payload=EXCLUDED.payload,fetched_at=EXCLUDED.fetched_at
WHERE mention_rosters.fetched_at <= EXCLUDED.fetched_at;

-- name: MentionToken :one
SELECT chat_id,person_id FROM mention_tokens WHERE account_id=$1 AND token=$2;

-- name: SaveMentionToken :one
INSERT INTO mention_tokens(account_id,token,chat_id,person_id) VALUES($1,$2,$3,$4)
ON CONFLICT(account_id,token) DO UPDATE SET token=EXCLUDED.token
RETURNING chat_id,person_id;
-- name: LockContactDirectory :exec
SELECT account_id FROM bridge_identity WHERE account_id=$1 FOR UPDATE;
