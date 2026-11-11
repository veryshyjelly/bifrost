-- +goose Up
CREATE TABLE mirror_choices (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 choice_id text NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(account_id,choice_id)
);
CREATE TABLE mirror_chat_requests (
 account_id text NOT NULL REFERENCES bridge_identity(account_id),
 request_id integer NOT NULL CHECK(request_id>0),
 payload jsonb NOT NULL,
 used_job_id bigint REFERENCES jobs(id),
 selected_chat bigint,
 PRIMARY KEY(account_id,request_id)
);
-- Forward only: request consumption and scoped action evidence are durable.
