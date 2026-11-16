// Package openwa adapts the OpenWA Go SDK to bridge delivery, identity, metadata
// and archive workflows. Calls validate native receipts and classify failures
// through providers rather than exposing SDK errors. Sends carry the caller's
// durable idempotency key; uncertain outcomes remain distinct from safe retries.
package openwa
