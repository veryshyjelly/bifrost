-- +goose Up
-- Dropping the source backlog must not enqueue it again through a webhook retry.
CREATE TABLE legacy_ingress_dedup (
 account_id text NOT NULL REFERENCES legacy_imports(account_id),
 dedup text NOT NULL,
 PRIMARY KEY(account_id,dedup)
);
INSERT INTO legacy_ingress_dedup(account_id,dedup)
SELECT p.account_id,convert_from(r.raw_row,'UTF8')::jsonb->>'dedup'
FROM legacy_cutover_policies p JOIN legacy_rows r ON r.account_id=p.account_id
WHERE p.queue_mode='fresh' AND r.source_table='jobs';
CREATE TRIGGER legacy_ingress_dedup_immutable BEFORE UPDATE OR DELETE ON legacy_ingress_dedup
 FOR EACH ROW EXECUTE FUNCTION protect_legacy_evidence();
