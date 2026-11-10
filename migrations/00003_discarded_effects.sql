-- +goose Up
ALTER TABLE effects DROP CONSTRAINT effects_state_check;
ALTER TABLE effects ADD CONSTRAINT effects_state_check
    CHECK (state IN ('pending','sending','done','uncertain','discarded'));
-- A discarded, definitively undelivered branch must never become a pending
-- mutation during rollback. Restore a validated backup for a schema downgrade.
