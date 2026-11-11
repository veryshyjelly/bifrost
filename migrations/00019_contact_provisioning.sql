-- +goose Up
CREATE TABLE contact_provisions (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 person_id text NOT NULL,
 session_id text NOT NULL,
 chat_id text NOT NULL,
 state text NOT NULL CHECK(state IN ('pending','bound')),
 PRIMARY KEY(account_id,person_id)
);
CREATE TABLE contact_bind_choices (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 choice_id text NOT NULL,
 person_id text NOT NULL,
 bot_id text NOT NULL,
 session_id text NOT NULL,
 owner_id bigint NOT NULL,
 plan_key text NOT NULL,
 expires_at bigint NOT NULL,
 used boolean NOT NULL DEFAULT false,
 PRIMARY KEY(account_id,choice_id),
 FOREIGN KEY(account_id,bot_id) REFERENCES telegram_credentials(account_id,bot_id)
);
-- Forward only: pending provisioning and consumed choices survive restarts.
