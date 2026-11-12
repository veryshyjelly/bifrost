-- +goose Up
ALTER TABLE metadata_runs DROP CONSTRAINT metadata_runs_source_check;
ALTER TABLE metadata_runs ADD CONSTRAINT metadata_runs_source_check CHECK(source IN ('command','cron','new','link','bind'));
-- Forward only: original automatic trigger labels remain durable report evidence.
