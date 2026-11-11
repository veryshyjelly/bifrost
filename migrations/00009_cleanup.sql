-- +goose Up
ALTER TABLE telegram_observations ADD COLUMN cleanup_error text NOT NULL DEFAULT '';
CREATE TABLE control_clears (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    clear_id text NOT NULL,
    payload jsonb NOT NULL,
    PRIMARY KEY (account_id,clear_id)
);
CREATE TABLE control_clear_items (
    account_id text NOT NULL,
    clear_id text NOT NULL,
    observation_seq bigint NOT NULL REFERENCES telegram_observations(seq),
    PRIMARY KEY (account_id,clear_id,observation_seq),
    FOREIGN KEY (account_id,clear_id) REFERENCES control_clears(account_id,clear_id)
);
CREATE TABLE cleanup_steps (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    job_id bigint NOT NULL REFERENCES jobs(id),
    payload jsonb NOT NULL,
    PRIMARY KEY (account_id,job_id)
);
-- Backfill existing timed notices without assigning expiry to archived chats.
INSERT INTO jobs(account_id,dedup,stream,kind,payload,due)
SELECT account_id,'control-expire:'||scope||':'||chat_id||':'||message_id,
       'control-expire:'||scope||':'||chat_id||':'||message_id,'control-expire',
       jsonb_build_object('scope',scope,'chat',chat_id,'message',message_id,'bot',CASE WHEN chat_id<0 THEN 'master' ELSE bot_id END),
       to_timestamp(expires_at::double precision/1000)
FROM telegram_observations WHERE expires_at>0 AND kind IN ('log','command') AND NOT deleted
ON CONFLICT(account_id,dedup) DO NOTHING;
-- Cleanup evidence and fixed snapshots must survive rollback.
