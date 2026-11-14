# Retire completed import tooling

The running Go installation owns the migrated state. Remove the obsolete
archive validator, staging/projection/activation CLI and implementation, their
import-only SQL queries and tests, and the retired deployment documentation.

Keep applied Goose migrations unchanged. Retain the runtime's read-only encrypted
configuration checks, activation barrier, scoped deduplication, logout proofs,
history completion evidence, reaction choices and Status skip/mute evidence.
Use current retained-state terminology for those protections. Do not change
existing database contents, permanent keys, frozen plans or delivery receipts.

Rewrite the README around the current runtime and document the tested Termux,
ADB/SSH, dashboard, boot and Android process-control setup.
