-- +goose Up
CREATE TABLE metadata_runs (
 ordinal bigserial NOT NULL,
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 run_id text NOT NULL,
 session_id text NOT NULL,
 scope text NOT NULL CHECK(scope IN ('all','group','contact')),
 source_id text NOT NULL,
 source text NOT NULL CHECK(source IN ('command','cron')),
 state text NOT NULL CHECK(state IN ('queued','running','completed')),
 PRIMARY KEY(account_id,run_id)
);
CREATE INDEX metadata_active_runs ON metadata_runs(account_id,session_id,ordinal) WHERE state<>'completed';
CREATE TABLE metadata_admissions (
 account_id text NOT NULL,
 request_key text NOT NULL,
 run_id text NOT NULL,
 PRIMARY KEY(account_id,request_key),
 FOREIGN KEY(account_id,run_id) REFERENCES metadata_runs(account_id,run_id)
);
CREATE TABLE metadata_run_requests (
 ordinal bigserial NOT NULL,
 account_id text NOT NULL,
 run_id text NOT NULL,
 job_id bigint NOT NULL REFERENCES jobs(id),
 PRIMARY KEY(account_id,run_id,job_id),
 FOREIGN KEY(account_id,run_id) REFERENCES metadata_runs(account_id,run_id)
);
CREATE TABLE metadata_run_targets (
 account_id text NOT NULL,
 run_id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('group','contact')),
 source_id text NOT NULL,
 source_key text NOT NULL,
 job_id bigint NOT NULL REFERENCES jobs(id),
 outcome text NOT NULL DEFAULT 'queued' CHECK(outcome IN ('queued','done','warning')),
 details jsonb NOT NULL DEFAULT '{}',
 PRIMARY KEY(account_id,run_id,kind,source_id),
 UNIQUE(account_id,job_id),
 FOREIGN KEY(account_id,run_id) REFERENCES metadata_runs(account_id,run_id)
);
CREATE INDEX metadata_target_identity ON metadata_run_targets(account_id,kind,source_id,job_id);
CREATE INDEX metadata_target_source ON metadata_run_targets(account_id,kind,source_key,job_id);
CREATE TABLE metadata_cron_ticks (
 account_id text NOT NULL,
 session_id text NOT NULL,
 expression text NOT NULL,
 minute bigint NOT NULL,
 run_id text NOT NULL,
 PRIMARY KEY(account_id,session_id,expression,minute),
 FOREIGN KEY(account_id,run_id) REFERENCES metadata_runs(account_id,run_id)
);
-- Forward only: run membership, outcomes, admissions and scheduled minutes persist.
