-- +goose Up
CREATE TABLE bridge_identity (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  account_id text NOT NULL UNIQUE CHECK (account_id <> ''),
  key_fingerprint text NOT NULL
);
CREATE TABLE jobs (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  account_id text NOT NULL REFERENCES bridge_identity(account_id),
  dedup text NOT NULL CHECK (dedup <> ''),
  stream text NOT NULL CHECK (stream <> ''),
  kind text NOT NULL CHECK (kind <> ''),
  payload jsonb NOT NULL,
  state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','running','done','failed','uncertain')),
  attempts integer NOT NULL DEFAULT 0,
  due timestamptz NOT NULL DEFAULT now(),
  claim_token text NOT NULL DEFAULT '',
  lease_until timestamptz,
  created timestamptz NOT NULL DEFAULT now(),
  UNIQUE (account_id, dedup)
);
CREATE INDEX jobs_ready ON jobs (state, due, id);
CREATE INDEX jobs_stream ON jobs (account_id, stream, id) WHERE state <> 'done';
CREATE TABLE effects (
  account_id text NOT NULL REFERENCES bridge_identity(account_id),
  effect_key text NOT NULL CHECK (effect_key <> ''),
  job_id bigint NOT NULL REFERENCES jobs(id),
  state text NOT NULL CHECK (state IN ('pending','sending','done','uncertain')),
  request jsonb NOT NULL,
  result jsonb,
  PRIMARY KEY (account_id, effect_key)
);
CREATE INDEX effects_job ON effects(job_id);
-- Deliberately no destructive Down migration: rollback requires a validated backup.
