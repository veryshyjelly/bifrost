-- +goose Up
CREATE TABLE metadata_snapshots (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 session_id text NOT NULL,
 source_id text NOT NULL,
 job_id bigint NOT NULL REFERENCES jobs(id),
 fetched_at bigint NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,session_id,source_id)
);
CREATE TABLE metadata_text_plans (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,job_id)
);
CREATE TABLE metadata_topic_names (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 session_id text NOT NULL,
 source_id text NOT NULL,
 chat_id bigint NOT NULL,
 thread_id bigint NOT NULL,
 return_key text NOT NULL,
 name text NOT NULL,
 PRIMARY KEY(account_id,session_id,source_id,chat_id,thread_id,return_key)
);
-- Forward only: frozen text requests and provider receipts are retained.
