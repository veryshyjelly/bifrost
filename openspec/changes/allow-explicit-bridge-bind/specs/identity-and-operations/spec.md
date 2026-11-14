## ADDED Requirements

### Requirement: Explicit HTTP bind address
Both `run` and `serve` SHALL use optional `BRIDGE_BIND_HOST` as an IP literal.
Omission SHALL preserve the wildcard listener. Invalid values SHALL fail without
echoing their contents. Authentication, ownership and provider behavior SHALL
remain unchanged. `healthcheck` SHALL probe the selected IP, using loopback for
wildcard addresses, without requiring provider or database credentials.

#### Scenario: Native deployment uses loopback
- **WHEN** the operator sets `BRIDGE_BIND_HOST=127.0.0.1`
- **THEN** the HTTP lifecycle listens only on that address and its configured port

#### Scenario: Existing deployment omits the bind address
- **WHEN** `BRIDGE_BIND_HOST` is omitted
- **THEN** the HTTP lifecycle retains its existing wildcard listener

#### Scenario: Healthcheck uses the explicit local listener
- **WHEN** the bridge listens on an explicit IPv4 or IPv6 address
- **THEN** its healthcheck probes that address and configured port

#### Scenario: Invalid listener configuration is private
- **WHEN** the operator supplies a hostname or an address containing a port
- **THEN** startup refuses configuration without echoing the supplied value
