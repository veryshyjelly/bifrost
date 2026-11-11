-- +goose Up
CREATE TABLE telegram_credentials (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 bot_id text NOT NULL CHECK(bot_id='master' OR bot_id ~ '^[1-9][0-9]*$'),
 ciphertext text NOT NULL CHECK(ciphertext<>''),
 revision text NOT NULL CHECK(revision<>''),
 identity jsonb NOT NULL,
 created_notice boolean NOT NULL DEFAULT false,
 PRIMARY KEY(account_id,bot_id)
);
CREATE TABLE telegram_registrations (
 account_id text NOT NULL,
 job_id bigint NOT NULL REFERENCES jobs(id),
 bot_id text NOT NULL,
 revision text NOT NULL,
 PRIMARY KEY(account_id,job_id),
 FOREIGN KEY(account_id,bot_id) REFERENCES telegram_credentials(account_id,bot_id)
);
CREATE TABLE telegram_setup_plans (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 job_id bigint NOT NULL REFERENCES jobs(id),
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,job_id)
);
-- Forward only: encrypted credentials and migration evidence survive recovery.
