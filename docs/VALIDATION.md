# Validation

`make check` runs formatting verification, vet, race-enabled Go tests, the
repository file audit, Compose validation and SQL generation drift checks.
`make spec-check` validates the behavior contracts and change proposals.

Persistence and concurrency tests require an isolated PostgreSQL instance through
`BIFROST_TEST_DATABASE_URL`; without it, those tests skip explicitly. Never use an
account database for automated tests. Fixtures cover duplicate events, retries,
uncertain sends, identity scope, callback ownership, topic guards and recovery.

Codec tests opt in through `BIFROST_TEST_CODECS=1` and require Pillow, ffmpeg and
Chromium. They cover animation, malformed duration metadata, transparency,
container validation, cache reuse and unsafe input handling. See
[sticker behavior](STICKER-LIMITS.md) for the encoder budget and cache policy.
The Termux launcher installer is tested in temporary directories for preserved
private state, service choices, boot ordering and symlink refusal.

Fixtures establish the behavior exercised by their assertions. They do not prove
recipient rendering, every provider workflow or long-term host reliability.
Provider acknowledgements alone do not prove delivery or display. Operators
should verify pairing, readiness, reboot recovery and message delivery on their
own installation, and keep the resulting operational records private.
