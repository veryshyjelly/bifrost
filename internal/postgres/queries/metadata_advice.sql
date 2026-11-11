-- name: MetadataInspection :one
SELECT payload FROM metadata_inspections WHERE account_id=$1 AND job_id=$2;
-- name: CreateMetadataInspection :exec
INSERT INTO metadata_inspections(account_id,job_id,payload) VALUES($1,$2,$3);
-- name: MetadataPermission :one
SELECT payload FROM metadata_permissions WHERE account_id=$1 AND action_id=$2;
-- name: CreateMetadataPermission :exec
INSERT INTO metadata_permissions(account_id,action_id,payload) VALUES($1,$2,$3);
-- name: MetadataReportJob :one
SELECT * FROM jobs WHERE account_id=$1 AND id=$2;
-- name: MetadataReportEffects :many
SELECT effect_key,state FROM effects WHERE account_id=$1 AND job_id=$2 ORDER BY effect_key;
-- name: CreateMetadataReport :exec
INSERT INTO metadata_reports(account_id,run_id,job_id,payload) VALUES($1,$2,$3,$4);
