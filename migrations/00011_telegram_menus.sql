-- +goose Up
-- Migration eligibility is explicit; registration and /start alone do not prove
-- that a bot has been migrated to the configured local Bot API server.
CREATE TABLE telegram_bot_lifecycle (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    bot_id text NOT NULL CHECK (bot_id='master' OR bot_id ~ '^[1-9][0-9]*$'),
    identity jsonb NOT NULL,
    migrated_at bigint NOT NULL CHECK (migrated_at > 0),
    menu_installed_at bigint NOT NULL DEFAULT 0,
    PRIMARY KEY(account_id,bot_id)
);
CREATE TABLE telegram_menu_plans (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    job_id bigint NOT NULL REFERENCES jobs(id),
    payload jsonb NOT NULL,
    PRIMARY KEY(account_id,job_id)
);
-- Forward only: preserve migration eligibility and original settings on retry.
