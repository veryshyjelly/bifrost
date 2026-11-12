-- +goose Up
CREATE TABLE source_action_jobs (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 job_id bigint NOT NULL REFERENCES jobs(id),
 source_job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,job_id),
 UNIQUE(account_id,source_job_id)
);
