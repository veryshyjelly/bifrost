-- +goose Up
CREATE TABLE relay_plans (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    plan_key text NOT NULL,
    job_id bigint NOT NULL REFERENCES jobs(id),
    direction text NOT NULL CHECK (direction IN ('in','out')),
    whatsapp_chat text NOT NULL,
    payload jsonb NOT NULL,
    complete boolean NOT NULL DEFAULT false,
    PRIMARY KEY (account_id,plan_key)
);
CREATE INDEX relay_plans_chat ON relay_plans(account_id,whatsapp_chat,direction);

CREATE TABLE message_mappings (
    seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    scope text NOT NULL,
    telegram_chat_id bigint NOT NULL,
    telegram_message_id bigint NOT NULL,
    thread_id bigint NOT NULL DEFAULT 0,
    bot_id text NOT NULL,
    whatsapp_chat text NOT NULL,
    whatsapp_message_id text NOT NULL,
    direction text NOT NULL CHECK (direction IN ('in','out')),
    plan_key text,
    part integer NOT NULL DEFAULT 0 CHECK (part >= 0),
    deleted boolean NOT NULL DEFAULT false,
    receipt jsonb NOT NULL,
    UNIQUE (account_id,scope,telegram_chat_id,telegram_message_id),
    UNIQUE (account_id,plan_key,part),
    FOREIGN KEY (account_id,plan_key) REFERENCES relay_plans(account_id,plan_key)
);
CREATE INDEX message_mappings_source ON message_mappings(account_id,whatsapp_chat,whatsapp_message_id);
ALTER TABLE telegram_observations ADD COLUMN deleted boolean NOT NULL DEFAULT false;
-- Completed delivery knowledge must not be discarded by schema rollback.
