-- +goose Up
CREATE TABLE message_reactions (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 message_id text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,whatsapp_chat,message_id)
);
CREATE TABLE reaction_destinations (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 message_id text NOT NULL,
 destination_key text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,whatsapp_chat,message_id,destination_key)
);
CREATE TABLE reaction_plans (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 plan_key text NOT NULL,
 job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL,
 complete boolean NOT NULL DEFAULT false,
 PRIMARY KEY(account_id,plan_key)
);
CREATE TABLE outgoing_reaction_plans (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 plan_key text NOT NULL,
 job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,plan_key)
);
CREATE TABLE reaction_echoes (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 whatsapp_chat text NOT NULL,
 message_id text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,whatsapp_chat,message_id)
);
CREATE TABLE custom_reaction_emojis (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 custom_id text NOT NULL,
 emoji text NOT NULL,
 PRIMARY KEY(account_id,custom_id)
);
-- Native setters use frozen plans and fenced progress, never message-send effects.
