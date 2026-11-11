-- +goose Up
CREATE TABLE message_assets (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 message_id text NOT NULL,
 asset jsonb NOT NULL,
 PRIMARY KEY(account_id,whatsapp_chat,message_id)
);
CREATE TABLE native_poll_actions (
 account_id text NOT NULL,
 action_id text NOT NULL,
 plan_key text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,action_id),
 FOREIGN KEY(account_id,plan_key) REFERENCES relay_plans(account_id,plan_key)
);
-- Native packet plans share the existing relay plan ownership/mapping ledger.
-- Forward only: preserve original media assets and scoped poll actions.
