-- +goose Up
CREATE TABLE topic_reconciliations (
 account_id text NOT NULL REFERENCES bridge_identity(account_id), operation_id text NOT NULL,
 job_id bigint NOT NULL REFERENCES jobs(id), effect_key text NOT NULL,
 request jsonb NOT NULL, result jsonb NOT NULL,
 PRIMARY KEY(account_id,operation_id)
);
-- Forward only: explicit operator reconciliation evidence remains immutable.
