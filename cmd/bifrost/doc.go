// Command bifrost configures, migrates, diagnoses and runs the bridge.
//
// With no subcommand it starts passive diagnostic serve: authenticated ingress
// may persist work when enabled, but no worker or metadata scheduler dispatches
// it. The explicit run subcommand verifies saved provider identities and acquires
// account ownership before bootstrapping bots and starting delivery.
//
// Recovery commands call the authenticated HTTP control surface, so operator
// decisions use the same scoped guards and receipts as the running bridge.
// Schema migration is a separate, explicit command rather than a startup side
// effect. Configuration, account identity and encryption keys must survive restarts.
package main
