-- +goose Up
CREATE TABLE metadata_inspections (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,job_id)
);
CREATE TABLE metadata_permissions (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 action_id text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,action_id)
);
CREATE TABLE metadata_reports (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 run_id text NOT NULL,
 job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,run_id)
);
-- Forward only: scoped grants, reports and source/receipt evidence persist.
