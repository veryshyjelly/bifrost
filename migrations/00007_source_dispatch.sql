-- +goose Up
CREATE TABLE whatsapp_sources (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    job_id bigint NOT NULL REFERENCES jobs(id),
    whatsapp_chat text NOT NULL,
    payload jsonb NOT NULL,
    PRIMARY KEY (account_id,job_id)
);
CREATE INDEX whatsapp_sources_chat ON whatsapp_sources(account_id,whatsapp_chat);
CREATE TABLE whatsapp_self (
    account_id text PRIMARY KEY REFERENCES bridge_identity(account_id),
    person_id text NOT NULL CHECK (person_id ~ '^[0-9]{5,15}@c\.us$')
);
-- Source identity snapshots and verified self identity must survive rollback.
