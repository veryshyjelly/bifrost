# Spec Delta

## Purpose

Manage contact bots through the official Telegram workflow while keeping owner consent, contact association and delivery readiness explicit.

## ADDED Requirements

### Requirement: Official provisioning and refresh
Provisioning SHALL retain official Managed Bots creation links/events, managed-token retrieval, encryption, hosted/local API migration and webhook/menu installation. BotFather automation, MTProto and manual token substitution MUST NOT replace this contract.

#### Scenario: New managed bot event
- **WHEN** an authorized official creation event arrives
- **THEN** the bridge retrieves and seals the token, installs the required local integration and offers explicit pending-contact binding.

#### Scenario: Existing bot token refresh
- **WHEN** registration repeats for an existing bot
- **THEN** its token/webhook are refreshed compatibly, readiness is preserved and a new ambiguous binding is not inferred.

### Requirement: Explicit one-contact association
A new managed bot SHALL remain unbound until the owner selects a valid pending contact. Binding MUST reject reuse of a bot assigned to another person and preserve existing confirmation/checkpoint behavior.

#### Scenario: Multiple pending contacts
- **WHEN** creation completes while several contacts have pending requests
- **THEN** association is chosen explicitly rather than inferred from request or creation order.

#### Scenario: Stale or conflicting binding
- **WHEN** a pending request is no longer valid or the bot is already bound elsewhere
- **THEN** the conflicting association is refused without reassigning identities.

### Requirement: Readiness and safe fallback
Binding and private readiness SHALL remain separate. Unbound DMs MUST explain how to bind; a bound bot becomes ready through the existing private interaction, and unavailable readiness MUST preserve manager routing.

#### Scenario: Owner has not started the bound bot
- **WHEN** a WhatsApp message targets a bound but unready dedicated bot
- **THEN** it remains deliverable through the manager path without forcing a new credential or association.

### Requirement: Independent inbox retirement
Starting a ready dedicated DM or linking a mirror SHALL retire its old inbox through separate durable work. Shared topics and General MUST be protected, and retirement failure MUST NOT replay message delivery or automatically import history.

#### Scenario: Retirement refusal
- **WHEN** Telegram refuses old-topic cleanup after successful routing transition
- **THEN** messaging retains the new route and cleanup remains independent of accepted sends.
