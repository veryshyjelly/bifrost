# Manual mirror linking and return to management

`internal/control.Mirrors` implements manual `/link <WhatsApp group ID>` and `/unmirror` through authenticated native ingress, independent management jobs and the fenced worker in isolated fixtures. The private `/group` picker, its Start links, complete topic-header callbacks, metadata workers and final runtime composition remain pending. No live route, callback or provider message is changed by these tests.

## Command and permission checks

Commands preserve master/member addressing, owner checks, caption parsing and ignored command/former-command edits. Manual linking uses the first argument and retains the legacy treatment of additional arguments. It requires a known WhatsApp group and a separate Telegram group. Native `getChat`, `getMe` and `getChatMember` verify the destination's identity, group type, privacy and the master's administrator membership. Both the primary username and active usernames exclude public destinations. Member identity must match the queried master. The [SDK chat model](https://github.com/go-telegram/bot/blob/v1.28.0/models/chat.go#L110) retains these native fields and 64-bit IDs.

Unmirror requires the owner, master bot and current linked WhatsApp mirror group, with no extra arguments. Invalid contexts receive the original guidance. Source control plans, command expiry and management admission commit together. The source command does not move the route or create a topic. Legacy management payloads with string Telegram IDs and native integer IDs are accepted without floating-point conversion.

## Link persistence

Link preparation freezes the requested destination and original route/return generation before provider checks. The commit serializes through the existing account-directory lock, rejects an occupied destination or conflicting route move, remembers the former management inbox, and commits the whole-group route with its complete confirmation plan. Linking selects `group` mode, matching the baseline. Route names, bindings, mappings and historical copies remain intact.

The independent retirement job and group metadata work are admitted in that same transaction. Delivery and cleanup do not depend on the confirmation succeeding. Confirmation retry keeps its original text/destination and receipts without re-applying the route over a later owner decision. Metadata execution remains a separate pending capability; its admitted work is retained rather than reported as completed.

## Unmirror persistence and history coordination

Migration `00012_mirror_transitions.sql` adds transition snapshots, return generations, persistent route-move reservations and history activity coordination. Initial return validation checks the exact mirror and prior return generation. Stale requests get the original stale-group notice. An import in the legacy active states `queued`, `fetching` or `delivering` gets the original wait guidance without creating a topic.

An eligible return reserves its conversation before invoking Telegram. No database transaction spans a provider call. Other route moves and new active imports take the shared account lock and respect that reservation. `Store.RecordHistoryActivity` is a fenced coordination boundary for future history handlers/import; it does not fetch history or replace message plans and receipts. Existing active imports must be restored before runtime forwarding is enabled.

Topic creation uses the existing effect ledger, the original management chat and the shared UTF-16 name budget. A confirmed receipt survives a local route-save failure, so recovery reuses that topic. An ambiguous creation remains uncertain for explicit review and retains its guard. A definite 400/403 refusal discards only the definitely undelivered effect, leaves the mirror active, releases the guard and persists the original permission guidance. Failure to save that guidance can recover from the discarded effect without attempting another topic creation.

After confirmed creation, the route, new return generation, remembered inbox, complete confirmation plan and fresh-topic notification admission commit together. Muted/ignored preferences are read again under the lock and preserved; other modes become `inbox`. Reserved/deleted or occupied topics cannot become the new target. The guard releases only with this committed move or a confirmed refusal. The original mirror messages, mappings and group membership remain intact. New relays and replies use management; the former mirror no longer sends to WhatsApp.

The management-topic URL button and exact legacy confirmation are retained. Configured command and interactive lifetimes freeze with the transition, preserving the 60/600-second defaults and custom values on retry. If a later link makes a pending return confirmation obsolete, only definitely unsent notice effects close; completed receipts are reconciled and sending/uncertain effects still require review. Return generations reject an old request even after the destination leaves and returns to the same mirror group.

## Remaining integration

The return admits a `new_conversation` job carrying the fresh chat/thread/return key. Full header/button restoration and its metadata workflow still need handlers; execution must respect that snapshot and any later route change. The private group picker must add its existing owner, account, expiry and selection checks before using these transitions; picker requests are explicitly unsupported by the manual path.

Generic job skipping does not reconcile an unknown topic-creation receipt or release its route reservation. Explicit transition reconciliation, the validated legacy importer and complete runtime workflow composition remain prerequisites for live startup. Import must preserve pending transitions, return generations, guards, history activity, mappings and all effect keys/receipts.

Tests exercise authenticated commands, native privacy/admin checks, whole-group targets, concurrent destination commits, independent deletion, archive preservation, paused/history guards, known/uncertain refusal, discarded-effect recovery, database reopening, obsolete notices and ABA return rejection. `/group` picker and full history/media delivery remain pending.
