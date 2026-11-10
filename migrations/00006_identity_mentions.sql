-- +goose Up
CREATE TABLE contact_aliases (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    alias text NOT NULL CHECK (alias ~ '^[0-9]+@lid$'),
    canonical text NOT NULL CHECK (canonical ~ '^[0-9]{5,15}@c\.us$'),
    PRIMARY KEY (account_id,alias)
);
CREATE INDEX contact_aliases_canonical ON contact_aliases(account_id,canonical);
CREATE TABLE contact_profiles (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    person_id text NOT NULL,
    name text NOT NULL,
    metadata_synced boolean NOT NULL DEFAULT false,
    PRIMARY KEY (account_id,person_id)
);
CREATE TABLE mention_rosters (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    chat_id text NOT NULL,
    payload jsonb NOT NULL,
    fetched_at bigint NOT NULL,
    PRIMARY KEY (account_id,chat_id)
);
CREATE TABLE mention_tokens (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    token text NOT NULL CHECK (token ~ '^[a-f0-9]{8,64}$'),
    chat_id text NOT NULL,
    person_id text NOT NULL,
    PRIMARY KEY (account_id,token)
);
-- Identity evidence and copied tokens must survive rollback for explicit review.
