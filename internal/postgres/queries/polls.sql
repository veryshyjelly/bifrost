-- name: NativePollAction :one
SELECT * FROM native_poll_actions WHERE account_id=$1 AND action_id=$2;
-- name: PollSelection :one
SELECT options FROM poll_selections WHERE account_id=$1 AND whatsapp_chat=$2 AND message_id=$3;
-- name: SetPollSelection :exec
INSERT INTO poll_selections(account_id,whatsapp_chat,message_id,options) VALUES($1,$2,$3,$4)
ON CONFLICT(account_id,whatsapp_chat,message_id) DO UPDATE SET options=EXCLUDED.options;
-- name: CreatePollVoteJob :exec
INSERT INTO poll_vote_jobs(account_id,job_id,source_job_id,payload) VALUES($1,$2,$3,$4);
-- name: PollVoteJob :one
SELECT * FROM poll_vote_jobs WHERE account_id=$1 AND job_id=$2;
-- name: SavePollVote :exec
UPDATE poll_vote_jobs SET plan=$3 WHERE account_id=$1 AND job_id=$2;
