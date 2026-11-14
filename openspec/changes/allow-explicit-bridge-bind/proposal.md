# Explicit bridge listen address

## Why

Native deployments do not have Compose's loopback port publication boundary.
Operators need to bind the authenticated HTTP listener to a specific local IP.

## What Changes

- Add optional `BRIDGE_BIND_HOST`, accepting an IPv4 or IPv6 literal.
- Apply the same configured listener to explicit `run` and passive `serve`.
- Preserve the existing wildcard default and private-value error handling.

## Impact

- Affects runtime configuration and listener composition only.
- Changes no provider, identity, queue, ingress-authentication or relay behavior.
