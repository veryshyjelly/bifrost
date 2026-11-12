-- +goose Up
CREATE TABLE operation_notice_settings (
 account_id text PRIMARY KEY REFERENCES bridge_identity(account_id), payload jsonb NOT NULL
);
-- Forward only: notification preferences and admitted notices survive restart.
