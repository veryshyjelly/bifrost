-- +goose Up
ALTER TABLE bridge_bots ADD COLUMN started boolean NOT NULL DEFAULT false;
ALTER TABLE bridge_routes
    ADD COLUMN name text NOT NULL DEFAULT '',
    ADD COLUMN mode text NOT NULL DEFAULT 'inbox' CHECK (mode IN ('inbox','dedicated','group','muted','ignored'));

CREATE TABLE bridge_contact_bots (
    account_id text NOT NULL,
    contact_id text NOT NULL CHECK (contact_id <> ''),
    bot_id text NOT NULL,
    PRIMARY KEY (account_id,contact_id),
    UNIQUE (account_id,bot_id),
    FOREIGN KEY (account_id,bot_id) REFERENCES bridge_bots(account_id,bot_id)
);
-- Bot tokens and verified phone/LID alias resolution are separate capabilities;
-- contact_id is the caller's canonical identity, never a display name.
