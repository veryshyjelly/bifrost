# Host transfer checklist

Stop all writers before exporting databases and assets. Preserve account identity,
encryption keys, queue state, stable delivery keys and original receipts. Verify
restored rows, sequence counters and asset hashes before provider startup.

Do not run paired identities concurrently on separate hosts: database ownership
locks cannot coordinate independent database copies. After transfer, verify
native identities, readiness, recovery and message delivery. Any rollback must
reconcile the latest paired-session and delivery state before restarting a host.

Keep archives, account-bound decisions and operational verification records private.
