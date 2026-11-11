-- name: MirrorChoice :one
SELECT payload FROM mirror_choices WHERE account_id=$1 AND choice_id=$2;
-- name: CreateMirrorChoice :exec
INSERT INTO mirror_choices(account_id,choice_id,payload) VALUES($1,$2,$3);
-- name: MirrorChatRequest :one
SELECT payload,used_job_id,selected_chat FROM mirror_chat_requests WHERE account_id=$1 AND request_id=$2;
-- name: CreateMirrorChatRequest :execrows
INSERT INTO mirror_chat_requests(account_id,request_id,payload) VALUES($1,$2,$3) ON CONFLICT DO NOTHING;
-- name: ConsumeMirrorRequest :execrows
UPDATE mirror_chat_requests SET used_job_id=$3,selected_chat=$4 WHERE account_id=$1 AND request_id=$2 AND used_job_id IS NULL;
