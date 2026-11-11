-- +goose Up
CREATE TABLE history_choices (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 choice_id text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,choice_id)
);
CREATE TABLE history_runs (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 run_id text NOT NULL,
 session_id text NOT NULL,
 whatsapp_chat text NOT NULL,
 state text NOT NULL CHECK(state IN ('queued','fetching','delivering','complete','failed')),
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,run_id)
);
CREATE UNIQUE INDEX history_one_active ON history_runs(account_id,session_id,whatsapp_chat) WHERE state IN ('queued','fetching','delivering');
CREATE TABLE history_items (
 account_id text NOT NULL,
 run_id text NOT NULL,
 item_index integer NOT NULL CHECK(item_index>=0),
 job_id bigint NOT NULL REFERENCES jobs(id),
 source_id text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','done','skipped')),
 payload jsonb NOT NULL,
 plan_key text,
 PRIMARY KEY(account_id,run_id,item_index),
 UNIQUE(account_id,job_id),
 FOREIGN KEY(account_id,run_id) REFERENCES history_runs(account_id,run_id),
 FOREIGN KEY(account_id,plan_key) REFERENCES relay_plans(account_id,plan_key)
);
CREATE TABLE message_tombstones (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 message_id text NOT NULL,
 revoked boolean NOT NULL DEFAULT false,
 PRIMARY KEY(account_id,whatsapp_chat,message_id)
);
-- Forward only: choices, frozen batches and destination receipts persist.
