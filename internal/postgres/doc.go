// Package postgres implements durable admission, fenced delivery, frozen workflow
// plans and recovery journals using PostgreSQL transactions and generated queries.
//
// Opening a Store only establishes a connection pool. Schema migration, permanent
// identity initialization and runtime ownership are explicit lifecycle operations.
// Workflow writes use claim checks and scope-specific locks; external provider
// calls belong to capability handlers, outside database transactions.
package postgres
