-- +goose Up
CREATE TABLE conversation_inboxes (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    whatsapp_chat text NOT NULL,
    telegram_chat_id bigint NOT NULL CHECK (telegram_chat_id < 0),
    thread_id bigint NOT NULL CHECK (thread_id > 1),
    PRIMARY KEY (account_id,whatsapp_chat,telegram_chat_id,thread_id)
);

CREATE TABLE inbox_retirements (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    job_id bigint NOT NULL REFERENCES jobs(id),
    payload jsonb NOT NULL,
    complete boolean NOT NULL DEFAULT false,
    PRIMARY KEY (account_id,job_id)
);

-- A reservation survives refusals and lost responses. Provider identifiers are
-- never reused as live targets while the deletion could already have happened.
CREATE TABLE retired_topics (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    telegram_chat_id bigint NOT NULL CHECK (telegram_chat_id < 0),
    thread_id bigint NOT NULL CHECK (thread_id > 1),
    whatsapp_chat text NOT NULL,
    deleted boolean NOT NULL DEFAULT false,
    PRIMARY KEY (account_id,telegram_chat_id,thread_id)
);

-- Forward only: deleting these reservations would make an ambiguous deletion
-- target eligible for new sends. Use a validated backup for rollback.
