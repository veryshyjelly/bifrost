# Runtime validation checklist

Run the automated checks documented at the selected source revision. Use isolated
databases and fixtures for tests; never run them against an account database.
Check startup, authentication, duplicate events, retries, uncertain sends and
recovery independently. Provider acknowledgement is not proof of recipient
delivery or rendering.

For a host transfer, stop writers, back up the current databases and assets,
preserve identity and keys, verify restored state and start only one runtime.
Keep installation-specific measurements, receipts and operational records private.
