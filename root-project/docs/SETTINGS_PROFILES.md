# Independent timing profiles (2026-10-17)

Setting has two sub-pages with independent Save actions:

- **Show Mode — Browser Chat:** Browser option 03 only. Reply delay min/max,
  optional no-reply auto skip (default OFF), no-reply timeout (default 60–60s),
  next-chat wait (default 0–0s). The opening greeting is not delayed by reply
  delay. DOM polling remains internal (350ms), not a user reply-speed setting.
- **Standard Bot — Start Bot:** Home Start Bot workers only. Existing poll,
  action, skip timeout, close wait and worker reply delay remain available.
  The worker engine, browser drivers, login, AI and proxy behavior are unchanged.

## Persistence and migration

Standard values stay in `settings/general`. Show Mode uses a separate Badger
key, `settings/show_mode/v1`. On first access, the old general reply delay is
copied once to Show Mode; auto skip stays OFF and next-chat wait stays zero.
The old worker reply delay is retained, not deleted. Future saves never write
one profile through the other profile's Save API. A store mutex serializes
Show Mode migration and saves. Saving standard settings first also freezes
migration before modifying legacy values.

Both forms validate integer seconds and min <= max, maximum 86400 seconds.
Show Mode requires a positive no-reply minimum when auto skip is enabled.

Show Mode copies timing values when its runner starts. Save then **close that
Show Mode browser and Run again**. Standard values apply after **Stop / Start
Bot**. Neither Save mutates an active runner's timings. Inline save/error
notices explain this; switching sub-pages preserves edits. Message modes,
usernames and proxies remain shared existing settings, not duplicated profiles.

## Timer semantics and safety

A confirmed bot send arms a randomly selected no-reply timeout. Accepted
partner messages clear it; a nonempty pending reply queue is processed before
any timeout decision. Reply delay is selected per accepted user message.
An additional snapshot check before a timeout skip lets a newly arrived
partner row cancel the timeout rotation. There is no server-level atomicity
claim: DOM changes can always occur after a snapshot.

Chat-to-lobby transitions schedule next-chat wait before START; skip transitions
also establish a wait for sites that move straight to another partner. Browser
START and rotation safety throttles remain in place, so timing is a minimum
intentional wait, not an exact delivery-time promise. New conversation IDs
clear old reply deadlines and timeout rotation state. Cancellation interrupts
reply sleep. Unconfirmed sends retain the original safe pause and do not arm
a fresh timeout. No browser/website API or WebSocket transport was added.

## Verification

- `npm test`: 69 PASS (including 4 new real-frontend settings tests).
- Modified Go sources parsed/formatted using the external WASM Go formatter.
- Added Go regression tests for migration/isolation, validation, application
  Save routing, timeout on/off, incoming-before-timeout, unconfirmed-send pause
  and next-chat waiting. **NOT_RUN here: no Go toolchain installed.**
- Native Windows/Wails build, Go race/vet and real-site acceptance: NOT_RUN.
  `npm run verify` records missing toolchains as BLOCKED, not PASS.

On Windows run `verify.bat`, then manually check both independent Save actions,
restart application to check persistence, and test Show Mode auto skip OFF/ON,
reply delay, delayed next START, late incoming messages and multiple browsers.
Never use real account data in synthetic test fixtures.
