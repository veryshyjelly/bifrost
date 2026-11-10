-- +goose Up
CREATE TABLE bridge_bots (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    bot_id text NOT NULL CHECK (bot_id <> ''),
    username text NOT NULL DEFAULT '',
    PRIMARY KEY (account_id, bot_id)
);

CREATE TABLE bridge_routes (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    whatsapp_chat text NOT NULL CHECK (whatsapp_chat <> ''),
    telegram_chat_id bigint NOT NULL CHECK (telegram_chat_id <> 0),
    thread_id bigint NOT NULL DEFAULT 0 CHECK (thread_id >= 0),
    inbox_retired boolean NOT NULL DEFAULT false,
    PRIMARY KEY (account_id, whatsapp_chat)
);
CREATE INDEX bridge_routes_telegram ON bridge_routes(account_id, telegram_chat_id, thread_id);

CREATE TABLE telegram_observations (
    seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    scope text NOT NULL,
    bot_id text NOT NULL,
    chat_id bigint NOT NULL,
    message_id bigint NOT NULL CHECK (message_id > 0),
    thread_id bigint NOT NULL DEFAULT 0,
    kind text NOT NULL CHECK (kind IN ('chat','command','service','protected','log')),
    message_date bigint NOT NULL,
    payload jsonb NOT NULL,
    UNIQUE (account_id, scope, chat_id, message_id)
);

-- +goose Down
DROP TABLE telegram_observations;
DROP TABLE bridge_routes;
DROP TABLE bridge_bots;
