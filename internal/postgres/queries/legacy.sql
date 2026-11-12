-- name: LegacyImport :one
SELECT * FROM legacy_imports WHERE account_id=$1;

-- name: CreateLegacyImport :exec
INSERT INTO legacy_imports(account_id,archive_digest,sqlite_digest,manifest,configuration_ciphertext,report)
VALUES($1,$2,$3,$4,$5,$6);

-- name: FinishLegacyPreparation :exec
UPDATE legacy_imports SET report=$2 WHERE account_id=$1 AND activated_at IS NULL;

-- name: ProjectLegacyValues :exec
INSERT INTO legacy_values(account_id,kind,source_id,source_value)
SELECT source.account_id, v->>'kind',v->>'id',v->>'value'
FROM (SELECT r.account_id,convert_from(r.raw_row,'UTF8')::jsonb AS v FROM legacy_rows r
 WHERE r.account_id=sqlc.arg(account_id) AND r.source_table='kv') source;

-- name: LegacyValue :one
SELECT source_value FROM legacy_values WHERE account_id=$1 AND kind=$2 AND source_id=$3;

-- name: LegacyActivationPending :one
SELECT EXISTS(SELECT 1 FROM legacy_imports WHERE account_id=$1 AND activated_at IS NULL);
