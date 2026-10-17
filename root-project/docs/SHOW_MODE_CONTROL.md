# Show Mode — explicit controls (2026-10-17)

This supersedes the earlier automatic-after-login Show Mode instructions.

## One place to manage Show Mode

**Browser → Show Mode — Browser Chat** now contains the timing/first-sender
settings, Save, Open Browser / Login, browser/account status, and per-browser
Start Show Chat / Stop Show Chat. Setting contains Standard Bot settings and
a link back to Browser, not another editable Show form. Home buttons explicitly
say Start/Stop **Standard Bot**. The sidebar reports Standard and Show status
separately. Common Profile/Count apply only to opening browsers.

1. Choose Message mode/content on Message and save it.
2. In Browser → Show Mode set first sender, first timeout, reply delay,
   no-reply skip and next-chat wait; Save.
3. Open Browser / Login. Log in manually. This does NOT send, Skip or click
   START. Status is Login pending, then Ready — Chat stopped.
4. Press Start Show Chat on the logged-in browser's row. Start loads and
   validates saved timing settings and freezes Message settings for this run.
5. Inspect the row's actual **Running settings**, message mode, first-sender
   decision and progress. These values come from the backend runner snapshot,
   not the editable form. Snapshot refresh is roughly once per second.
6. Stop Show Chat cancels and drains the runner, keeping the logged-in browser
   open. Stop all Show chats, edit, Save, Start again. No browser restart needed.
7. Close the native browser to end that browser session.

Unsaved timing edits block Open/Start in the UI; server Start also checks the
saved revision and readiness. A stale revision is rejected, never silently
replaced by default values. Saves are rejected on the backend while any Show
chat is starting/running/stopping. UI controls are locked during those phases.
Changing Message settings does not change an already-running Show engine.

## First messages, history and timers

Explicit Start snapshots existing DOM rows as history. They are NOT replayed
as new input. If that existing conversation already has history, the runner
waits for a new partner message rather than adding an unsolicited opener.
New conversations use Bot/User/Random as configured. Incoming rows seen before
a possible opener take priority; DOM/server ordering cannot be atomic.
Random decisions are once per conversation. The displayed decision explains
when an existing chat or an already-arrived message takes precedence.

User-first replies use the configured reply delay. Follow-up lines returned by
the same Show responder also use that range (the hidden fixed 400ms gap is
removed). Unsolicited opening greetings have no reply delay. Browser detection,
queueing and native send confirmation can add time: these are minimum intended
waits, not exact delivery deadlines.

First-message timeout starts while waiting for the first partner message;
no-reply timeout starts only after confirmed bot sends. New incoming messages
and pending replies take priority over timeout skip. Next-chat wait still
applies after chat end/skip. Stop cancels pending sleeps; ownership is retained
until the old runner is fully drained. Already-submitted native side effects
cannot be retracted. Generation checks, author classification, dedupe and
uncertain-send safety pauses remain in place.

## Account ownership and scope

Within this desktop App, Show and Standard workers acquire one owner per
account ID. A second Show browser or Standard worker cannot automate that same
account concurrently. Standard saved-account preflight reports known conflicts;
its runtime guard also checks after login, including newly captured identities,
before registration/realtime/chat automation. Ownership releases only when the
owning worker/runner exits. Other accounts may run independently. Clean Profiles
is blocked while Show browsers are open or automation ownership is still held.

This is an **in-process guard**, not a cross-machine/account lock. Close older
EXEs, separately running CLI bots and other bot instances before live retesting;
those external processes cannot be stopped or detected by this App's registry.
Ready login browsers are not automation owners until Start is pressed.

Browser drivers/selectors, proxy, login authentication, license, API transport,
EVA/AI engines and standard reply logic are unchanged. `internal/bot/run.go`
only gains the optional desktop ownership hook; CLI behavior is unchanged.
The Show lifecycle is isolated in `show_control.go`, with narrow App wiring.

## Evidence

- Actual frontend Node suite: **76 PASS**, including placement, dirty state,
  backend revision routing, active-run summary/locking, conflict rendering and
  stale response handling. Safe text rendering is tested.
- Temporary source copy with pinned GitHub mirrors: Go model/bot/uistore race
  tests and production App/Show/transcript/runner race tests PASS. New cases:
  login without auto-chat; saved User first and 3–6s pending reply cancellation;
  actual fixed 1s reply delay; frozen message settings; first timeout and next
  wait; old history on Start/restart; stale revisions; active-save rejection;
  same-account conflicts and different-account allowance; Stop drain barrier;
  real capture/save/idle lifecycle with synthetic backend; Standard guard refusal
  before realtime/automation. Windows WMI hardware access is a panic-only test
  boundary in the Linux selected-source harness; license source is untouched.
- Temporary-copy full Windows cross-build, Windows-target root test compilation
  and scoped Go vet PASS, with original production sources and pinned mirrors.
- Full standard release gate, native Windows/Wails interaction and real-site
  acceptance are not established by those isolated checks. See current
  verification-output/report.json for the standard gate's exact status.

Manual acceptance on Windows: close older bot processes, use User first,
5–8s first wait, 3–6s reply delay, no-reply 10–20s, next wait 5–8s. Verify no
activity after login until Start; actual running values and decision; no reply
to pre-Start history; correct first reply delay; Stop while delay is pending;
Save/restart without closing the browser; separate-account operation and
same-account conflicts; native browser closure; ordinary Rod/ChromeDP and
Standard behavior unchanged. Use a new build folder and retain old data backups.
