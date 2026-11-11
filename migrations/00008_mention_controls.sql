-- +goose Up
CREATE TABLE control_plans (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    plan_key text NOT NULL,
    job_id bigint NOT NULL REFERENCES jobs(id),
    payload jsonb NOT NULL,
    complete boolean NOT NULL DEFAULT false,
    PRIMARY KEY (account_id,plan_key)
);
CREATE TABLE control_actions (
    account_id text NOT NULL REFERENCES bridge_identity(account_id),
    action_id text NOT NULL,
    plan_key text NOT NULL,
    payload jsonb NOT NULL,
    PRIMARY KEY (account_id,action_id),
    FOREIGN KEY (account_id,plan_key) REFERENCES control_plans(account_id,plan_key)
);
ALTER TABLE telegram_observations ADD COLUMN expires_at bigint NOT NULL DEFAULT 0;
CREATE INDEX telegram_observations_expiry ON telegram_observations(account_id,expires_at)
WHERE expires_at>0 AND NOT deleted;
-- Plans, action bindings and cleanup evidence must survive rollback.
