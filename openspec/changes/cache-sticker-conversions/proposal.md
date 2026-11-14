# Cache successful sticker conversions

Sticker conversion can be expensive on resource-constrained hosts. Converted bytes
are retained, but new messages with the same source invoke the codec again.

Add a persistent source-content/target/codec-policy lookup into the existing
hash-addressed asset store. Verify source and output integrity before reuse,
coalesce concurrent requests and publish only successful conversion entries.
Keep animation/timing, provider budgets, original-document fallbacks and frozen
delivery plans unchanged. The first conversion remains necessary; the cache warms
with the corrected codec and does not adopt older unversioned conversions.
