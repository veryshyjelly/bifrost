-- +goose Up
CREATE TABLE message_change_plans (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 plan_key text NOT NULL,
 job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL,
 complete boolean NOT NULL DEFAULT false,
 PRIMARY KEY(account_id,plan_key)
);
CREATE TABLE message_latest_edits (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 message_id text NOT NULL,
 body text NOT NULL,
 mentioned_ids jsonb,
 job_id bigint NOT NULL REFERENCES jobs(id),
 PRIMARY KEY(account_id,whatsapp_chat,message_id)
);
CREATE TABLE message_outbound_edits (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 message_id text NOT NULL,
 body_hash text NOT NULL,
 PRIMARY KEY(account_id,whatsapp_chat,message_id)
);
-- Confirmed edits and original presentation survive restarts and schema upgrades.
