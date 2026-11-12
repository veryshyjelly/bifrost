-- +goose Up
CREATE TABLE poll_selections (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 message_id text NOT NULL,
 options jsonb NOT NULL,
 PRIMARY KEY(account_id,whatsapp_chat,message_id)
);
CREATE TABLE poll_vote_jobs (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 job_id bigint NOT NULL REFERENCES jobs(id),
 source_job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL,
 plan jsonb,
 PRIMARY KEY(account_id,job_id)
);
-- Native poll actions retain the original account/session/destination scope.
