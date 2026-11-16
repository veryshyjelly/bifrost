// Package bridge dispatches durable native events to conversation capabilities
// through narrow storage/provider interfaces and the fenced delivery worker.
//
// The text path checks saved plans and echoes before fresh identity/provider
// lookups. Optional capabilities are injected as extensions; constructing the
// dispatcher does not start workers, install webhooks or acquire runtime ownership.
package bridge
