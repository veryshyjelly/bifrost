-- +goose Up
CREATE TABLE mirror_returns (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 return_key text NOT NULL,
 PRIMARY KEY(account_id,whatsapp_chat)
);
CREATE TABLE mirror_transitions (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,job_id)
);
-- An unresolved topic creation must not race another route move or import.
CREATE TABLE route_transition_locks (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 job_id bigint NOT NULL REFERENCES jobs(id),
 PRIMARY KEY(account_id,whatsapp_chat)
);
-- Coordination only: history message plans/receipts remain a separate workflow.
CREATE TABLE history_activity (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 run_id text NOT NULL,
 status text NOT NULL CHECK(status IN ('queued','fetching','delivering','complete','failed','cancelled')),
 PRIMARY KEY(account_id,whatsapp_chat)
);
-- Forward only: retain unresolved transitions, return generations and guards.
