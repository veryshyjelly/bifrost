-- +goose Up
CREATE TABLE revocation_tasks (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 task_id text NOT NULL,
 whatsapp_chat text NOT NULL,
 message_id text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,task_id)
);
-- Tombstones live in message_tombstones and are never reversed by a Keep choice.
