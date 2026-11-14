## ADDED Requirements

### Requirement: Persistent sticker conversion reuse
The bridge SHALL reuse successful sticker conversion bytes for the same verified source content, destination and codec policy across messages and restarts. Cache hits SHALL verify retained source and output integrity without repeating rendering or transcoding. Cache operation SHALL preserve animation, provider limits, original-file fallback and immutable delivery plans.

#### Scenario: Repeated sticker after restart
- **WHEN** a sticker was successfully converted under the current codec policy and the same bytes are received again after restart
- **THEN** the retained converted asset and its static, animated or video format are reused without codec subprocesses

#### Scenario: Different destination or codec policy
- **WHEN** the source bytes, destination or conversion policy differ from an existing lookup
- **THEN** that lookup is not reused for the new conversion

#### Scenario: Missing or damaged cache
- **WHEN** a lookup or its retained output is missing, invalid or fails integrity checks
- **THEN** it is ignored and normal conversion and original-document fallback rules apply
- **AND** no frozen delivery asset or receipt is rewritten

#### Scenario: Concurrent requests and cancellation
- **WHEN** concurrent requests in the runtime need the same conversion
- **THEN** they share one in-progress conversion
- **AND** a cancelled waiter does not cancel the active caller's conversion
- **AND** failures and cancelled conversions do not become reusable cache entries

#### Scenario: Unwritable cache index
- **WHEN** conversion succeeds but its cache lookup cannot be published
- **THEN** the successful converted asset remains available for normal delivery
