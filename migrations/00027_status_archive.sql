-- +goose Up
CREATE TABLE status_feeds (
 account_id text NOT NULL REFERENCES bridge_identity(account_id), management bigint NOT NULL,
 job_id bigint NOT NULL REFERENCES jobs(id), payload jsonb NOT NULL, complete boolean NOT NULL DEFAULT false,
 PRIMARY KEY(account_id,management)
);
CREATE TABLE status_posts (
 account_id text NOT NULL REFERENCES bridge_identity(account_id), post_id text NOT NULL,
 job_id bigint NOT NULL REFERENCES jobs(id), payload jsonb NOT NULL, source jsonb NOT NULL,
 skipped boolean NOT NULL DEFAULT false, complete boolean NOT NULL DEFAULT false,
 PRIMARY KEY(account_id,post_id)
);
CREATE TABLE status_refreshes (
 account_id text NOT NULL REFERENCES bridge_identity(account_id), job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL, complete boolean NOT NULL DEFAULT false, PRIMARY KEY(account_id,job_id)
);
CREATE TABLE status_mutes (
 account_id text NOT NULL REFERENCES bridge_identity(account_id), author text NOT NULL,
 payload jsonb NOT NULL, PRIMARY KEY(account_id,author)
);
CREATE TABLE status_mute_counters (
 account_id text PRIMARY KEY REFERENCES bridge_identity(account_id), revision bigint NOT NULL
);
CREATE TABLE status_mute_operations (
 account_id text NOT NULL REFERENCES bridge_identity(account_id), job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL, PRIMARY KEY(account_id,job_id)
);
CREATE TABLE status_controls (
 account_id text NOT NULL REFERENCES bridge_identity(account_id), control_id text NOT NULL,
 plan_key text NOT NULL, payload jsonb NOT NULL, PRIMARY KEY(account_id,control_id)
);
CREATE TABLE status_control_edits (
 account_id text NOT NULL REFERENCES bridge_identity(account_id), job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL, complete boolean NOT NULL DEFAULT false, PRIMARY KEY(account_id,job_id)
);
-- +goose Down
DROP TABLE status_control_edits,status_controls,status_mute_operations,status_mute_counters,status_mutes,status_refreshes,status_posts,status_feeds;
