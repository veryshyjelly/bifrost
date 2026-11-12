-- name: TopicReviewJob :one
SELECT * FROM jobs WHERE id=$1;
-- name: LockTopicReviewJob :one
SELECT * FROM jobs WHERE id=$1 FOR UPDATE NOWAIT;
-- name: TopicReviewGuard :one
SELECT EXISTS(SELECT 1 FROM route_transition_locks WHERE account_id=$1 AND whatsapp_chat=$2 AND job_id=$3);
-- name: TopicReviewNativePlans :many
SELECT * FROM relay_plans WHERE account_id=$1 AND job_id=$2;
-- name: ReadTopicReviewEffect :one
SELECT * FROM effects WHERE account_id=$1 AND effect_key=$2;
-- name: TopicReconciliation :one
SELECT request,result FROM topic_reconciliations WHERE account_id=$1 AND operation_id=$2;
-- name: CreateTopicReconciliation :exec
INSERT INTO topic_reconciliations(account_id,operation_id,job_id,effect_key,request,result) VALUES($1,$2,$3,$4,$5,$6);
-- name: AdoptReviewedTopic :execrows
UPDATE effects SET state='done',result=$4 WHERE account_id=$1 AND job_id=$2 AND effect_key=$3 AND state IN ('pending','uncertain');
-- name: ResetReviewedTopic :execrows
UPDATE effects SET state='pending',result=NULL WHERE account_id=$1 AND job_id=$2 AND effect_key=$3 AND state IN ('pending','uncertain');
