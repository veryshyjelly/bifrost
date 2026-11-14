## ADDED Requirements

### Requirement: Current runtime installation surface
The CLI SHALL expose current configuration, schema migration, diagnostics, explicit runtime and recovery commands. The completed archive validation, preparation and activation commands SHALL be retired. Applied database migration history and all retained delivery/identity evidence SHALL remain unchanged.

#### Scenario: Retired command requested
- **WHEN** an operator requests a removed archive import command
- **THEN** the CLI reports an unsupported command without reading archives, changing state or contacting providers

#### Scenario: Existing migrated database
- **WHEN** the current runtime opens an already activated database
- **THEN** the same encrypted configuration, scoped deduplication, completed-history, reaction and Status evidence remain authoritative
- **AND** incomplete retained-state activation continues to block ordinary claims and runtime ownership
