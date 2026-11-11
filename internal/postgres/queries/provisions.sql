-- name: ContactProvisions :many
SELECT * FROM contact_provisions WHERE account_id=$1 ORDER BY person_id;
-- name: RequestContactProvision :exec
INSERT INTO contact_provisions(account_id,person_id,session_id,chat_id,state) VALUES($1,$2,$3,$4,'pending')
ON CONFLICT(account_id,person_id) DO UPDATE SET session_id=EXCLUDED.session_id,chat_id=EXCLUDED.chat_id,state='pending';
-- name: BindContactProvision :exec
UPDATE contact_provisions SET state='bound' WHERE account_id=$1 AND person_id=$2;
-- name: CreateContactBindChoice :exec
INSERT INTO contact_bind_choices(account_id,choice_id,person_id,bot_id,session_id,owner_id,plan_key,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8);
-- name: ContactBindChoice :one
SELECT * FROM contact_bind_choices WHERE account_id=$1 AND choice_id=$2;
-- name: UseContactBindChoice :exec
UPDATE contact_bind_choices SET used=true WHERE account_id=$1 AND choice_id=$2;
-- name: ProvisionMessageSender :one
SELECT evidence.payload FROM (
 SELECT w.payload,w.job_id FROM whatsapp_sources w WHERE w.account_id=sqlc.arg(account_id) AND w.whatsapp_chat=sqlc.arg(whatsapp_chat) AND w.payload->>'MessageID'=sqlc.arg(message_id)::text
 UNION ALL
 SELECT i.payload->'Source' AS payload,i.job_id FROM history_items i JOIN history_runs r ON r.account_id=i.account_id AND r.run_id=i.run_id
 WHERE i.account_id=sqlc.arg(account_id) AND r.whatsapp_chat=sqlc.arg(whatsapp_chat) AND i.source_id=sqlc.arg(message_id)::text
) evidence ORDER BY evidence.job_id LIMIT 1;
