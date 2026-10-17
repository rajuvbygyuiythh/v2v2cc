# Home: live settings and timing diagnostics (Show Mode)

The Home page keeps **Diagnostics & timing** collapsed by default. Open Details
to place it beside the transcript; below 1100px they stack. No new bot controls live in this panel.
Standard Bot is not instrumented and must not be interpreted as Show Mode.

## Selecting and reading

- Default: follow the selected transcript's diagnostic run and DOM conversation.
  Before a correlated transcript exists, the latest retained run is selected.
- The Browser / run selector can pin an earlier run. **All conversations in this
  run** includes its entire retained timeline. A pinned run with no matching
  selected transcript also displays all its conversations; the notice says so.
- Browser/login lifecycle records are separate from each successful Start's
  chat run. Login READY means the browser is usable, not that chat is running.
- Metadata comes from the backend: visible Rod Show Mode, browser/profile/account,
  message engine, run ID, saved revision and frozen settings loaded at Start.
  Unsaved form edits are never substituted. Stopped-run settings remain visible.
- Settings show bot/user/random first rule and probability, first-message timeout,
  reply delay, no-reply skip and next-chat wait. FIRST_TURN records the actual
  chosen rule; FIRST_TURN_OVERRIDE explains replying to an already-present user.
  HISTORY_IGNORED identifies the pre-Start baseline without recording its text.

## Timing definitions

All metric values are milliseconds in the structured API/export, seconds in the
panel. Timestamps are UTC ISO strings. Event `time` is when the event is recorded;
INCOMING_DETECTED additionally carries `detected_at`, sampled on the runner's
accepted DOM snapshot read, before bookkeeping/responder work. This is **not**
the website's exact sent/arrival time and cannot measure site-to-poll latency.

- Selected delay: the existing RNG choice, with configured min/max. Instrumentation
  does not draw another random value or change original timer/decision order.
- Actual intentional wait: elapsed around the existing cancellable reply or
  follow-up sleep, including scheduler overhead. A cancelled wait is partial.
- Queue wait: detection until that incoming row is dequeued for processing; the
  selected reply wait and send time are not counted as queue time.
- Detected → send started: includes queue, intentional wait, validation and reply
  generation. Detected → confirmed total also includes native typing/confirmation.
- Send / confirmation elapsed: existing SendDOMMessage call, including typing and
  its existing DOM confirmation. Confirmed requires that call's success boolean
  and no error. An unconfirmed send has no confirmed-total metric and remains
  safety-paused, without a new resend/retry mechanism.
- Timer armed → event elapsed: first-message/no-reply/next-chat timer elapsed
  until incoming, timeout, or observed START. Unlike a reply sleep this can
  include DOM polling/page availability, not just configured intentional waiting.
- Opener has no incoming-derived trace/totals. Follow-ups share their incoming
  reply trace, have a separate part number and a separately selected/measured wait.

A confirmed **own DOM row is not a delivery or read receipt**. Run-global events
remain visible under a conversation filter. Conversation labels in the timeline
are local to the currently retained/filtered view, not permanent transcript IDs.
Cancellation, partner changes, empty output, timeouts, skips and safety notices
are structured events; notices never parse or copy old log/error strings.

## Bounds, privacy and controls

In-memory only: up to **12 runs × 500 events**, with truncation, eviction and
nonblocking-append omission notices. A busy diagnostic snapshot cannot block a
runner's event append. Evicted explicit selections stay empty, never silently
switch to another run. No per-DOM-poll event stream or new persistent database.

- Auto-scroll can be disabled while reading.
- Copy selected run diagnostics fetches all retained conversations in that run,
  even if the view is filtered. Its explicit export schema omits profile/account,
  raw conversation identifiers, SMS text, credentials and arbitrary future fields;
  conversation identifiers become local `chat_1`, `chat_2`, etc. If clipboard access
  is unavailable a selectable read-only textarea is shown instead.
- Clear Diagnostic View clears selected-run events only. Frozen metadata/settings,
  transcript, other runs and **logs.txt** are untouched. Existing logging behavior
  is unchanged; this is a separate diagnostic feed, not a claim that legacy logs
  themselves contain no chat data.
- No API key, cookies, tokens, proxy passwords, raw SMS bodies or raw errors are
  added to the diagnostic feed. Local profile/account labels are intentionally
  shown in the panel, but omitted from Copy.

## Verification and scope

Targeted synthetic coverage exercises bounded storage, filtering, immutable
snapshots, clear/eviction, allowlisting, concurrent/nonblocking append, timing and
reply correlation, cancellation, unconfirmed sends, RNG neutrality, lifecycle
restart correlation, safe frontend rendering, export redaction, stale responses,
slow polling, copy fallback and clear failure. Full release verification still
requires the original modules/toolchain and native Windows acceptance. Isolated
Go dependency replacements are test evidence, not original-module certification.
No live browser/account/login test was run for this change.

Drivers, auth, proxy, license, AI/EVA decisions, Standard engine logic,
dependencies, installers and original logs.txt behavior are unchanged by the
diagnostic feed. Common settings are now shared after explicit setup; see
[SHARED_DASHBOARD.md](SHARED_DASHBOARD.md).
