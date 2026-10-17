# Simplified dashboard and genuinely shared settings

Owner decision, 2026-10-18: one shared Settings form, not two mode-specific copies
of the same form. This explicitly supersedes the previous independent-profile
requirement. Engines, drivers, auth, proxy, license and AI decisions are unchanged.

## Everyday flow

1. **Settings:** choose an existing profile on first setup, review values, Save.
2. **Messages:** choose the message engine/content and save when needed.
3. **Browser:** open a Show Mode browser and complete login manually.
4. **Home:** choose Show Mode and Start the appropriate logged-in browser. Or
   select Standard Bot and use its Start/Stop controls for background workers.

Switching the Home mode changes the view only. It does not start/stop a running
mode. Both mode status pills remain visible. Standard view never presents the
Show transcript as Standard data. Home's More menu contains refresh, UI-log
clear and profile cleanup (destructive cleanup now asks for confirmation).
Four main activity counters are visible; additional counters remain in More
activity. Diagnostics and timing are collapsed until requested. Advanced AI
configuration and mode-specific timing controls are also collapsed by default.

## What is shared

One form and one Save persist:

| Common setting | Standard engine view | Show runner view |
|---|---|---|
| First-message rule/probability/first timeout | FirstMessage | FirstMessage |
| Reply delay range | ReplyDelay* | ReplyDelay* |
| No-reply timeout range | MatchReplyTimeout* | NoReply* |
| Next-chat wait range | MatchCloseWait* | NextChat* |

Mode-specific controls in Advanced: Standard polling/action intervals and Show
Auto Skip. The no-reply **range** is shared; Show's enable/disable switch is not
applied to the Standard engine. Standard DMs retain their existing non-disconnect
behavior, and first-turn decisions remain per conversation. Equal numbers do not
make these engines identical or redefine their native delivery semantics.

## Existing data and persistence

`GetChatSettings` returns both legacy candidates until setup. The UI requires an
explicit Standard Bot or Show Mode choice; nothing is silently averaged or chosen
by the backend. Selecting a candidate is only a draft; old values remain saved
until Save. The chosen common values become both modes' settings. Standard's
poll/action values and Show's Auto Skip are preserved whichever candidate is
chosen. If a disabled Show timeout was zero, the UI retains the existing positive
Standard timeout, visibly shown for review, because Standard requires one.

One Badger transaction writes the two existing engine-compatible settings records
and `settings/chat_shared/v1`. No new database or dependency. Keeping compatible
records lets engines read their original structures without changing chat logic.
After the marker is set, **legacy SaveGeneralSettings and SaveShowModeSettings
also synchronize common fields**, atomically, instead of reintroducing divergent
profiles. Before setup their prior behavior is retained for compatibility.
Validation failures commit neither record nor marker. Reopening preserves sharing.
The migration is intentionally not an automatic rollback facility: after explicit
Save the selected common values replace both old profiles. Back up your existing
local data before installing a new build if you need those older values later.

Shared Save and legacy setters (after setup) reject changes while either mode is
running, a Show run is draining, or worker ownership remains after cancellation.
StartBot's transition to running is serialized with shared Save through showMu;
no settings can slip between its idle check and setting the running flag. Each
Show Start still freezes actual settings/revision, and diagnostics retain that
snapshot after Stop. A new shared Save increments the Show revision, so a stale
Start request is rejected. No browser restart is required merely to save settings.

## Source and verification

- `chat_settings.go`: Wails helper and idle/drain guards; wrappers in `app.go`.
- `internal/uistore/chat_settings.go`: mapping, validation, atomic persistence.
- `internal/model/ui.go`: ChatSettings DTO. No chat engine modifications.
- Frontend dist HTML/JS/CSS: unified form, Home mode view, collapsed details,
  draft/stale-request protection and responsive consistent dark styling.
- Tests: shared store migration/rollback/reopen/legacy/concurrency, App run guards,
  and UI single-form/save/migration/draft/lock/stale/validation/control behavior.

Node synthetic tests and isolated Go race/vet/Windows compilation are evidence,
not native acceptance. Original-module release verification may remain BLOCKED
in this sandbox; no real browser/account/login or native desktop run is implied.
