# Simplified dashboard: three Show Mode timing settings

Current owner decision, 2026-10-18: Show Mode has **only three timing groups**:

1. **Reply delay** — wait after detecting a user message before responding.
2. **Next chat wait** — wait after a skip/end before starting the next chat.
3. **User silent timeout** — one finite range used both before the first user SMS
   and throughout an active conversation. Silence always triggers auto skip.

Each group retains Min/Max seconds. Equal values mean a fixed duration. Reply and
next-chat waits may be zero. Silent timeout must be at least one second. There is
no separate first-message timeout, random/bot-first selector, Wait indefinitely,
or Auto Skip ON/OFF control. This supersedes earlier screenshots/instructions.

## Behavior

On a new match, Show uses the message engine's normal bot opener. If the partner
already wrote, reply to the incoming row instead. Pre-Start history stays ignored;
starting in an old conversation waits for a new user SMS, with the same finite
silent timeout instead of waiting forever. Existing legacy first-turn/random/
skip-disabled settings cannot secretly re-enable those removed Show behaviors.

The silent timer is armed at chat readiness, including when the engine returns no
opener. A detected incoming row clears the idle wait before queueing its reply.
The timer is rearmed after confirmed bot sends (last follow-up wins), or after
reply processing finishes with no output. Pending replies and follow-up sleeps
are not interrupted even if they exceed the configured idle timeout. A late
incoming row still wins the final pre-skip recheck. Partner changes discard the
old timer. Stop cancels pending actions; unconfirmed sends remain safely paused,
not automatically resent or blindly skipped. No DOM selector/retry/driver change.

Default migration for an old disabled zero Show idle range is 60–60 seconds.
Nonzero saved idle ranges, reply delays and next-chat waits remain unchanged.
Actual normalized run settings are frozen at Start and appear in diagnostics.

## Dashboard and saved data

- Home: contextual Show/Standard Start/Stop, chat and collapsed diagnostics.
- Browser: launch/login only. Login does not automatically start chatting.
- Settings: one form and one Save for the three shared timing ranges.
- Advanced: Standard polling/action intervals only, not additional Show controls.
- Mode switching changes the view only, never starts/stops an engine.

First setup still explicitly chooses existing Standard or Show **timing values**;
selection is a draft until Save. Standard's stored first-turn policy is preserved,
not copied from Show or reset by saving these three timings. Standard engine logic
(including DM behavior) is unchanged. The shared no-reply numeric range still
maps to Standard MatchReplyTimeout and Show NoReply; next wait maps to Standard
MatchCloseWait and Show NextChat. These are shared values, not identical engines.

The existing UI-store records and sharing marker commit atomically, without new
dependencies/databases. Legacy setters keep the timing ranges synchronized after
shared setup. Legacy first-turn/auto-skip JSON fields remain for old-client/data
compatibility but are not independent controls for managed Show. New dashboard
saves do not send an auto-skip choice; the backend forces it on. Save rejects either
running mode or draining ownership. A successful Save invalidates stale Start
revisions. Back up existing local data before upgrading if older values are needed.

## Verification

Targeted tests cover initial silence with ignored history, empty opener/reply,
mid-conversation silence, expired-timer incoming priority, reply/follow-up waits
longer than the silent timeout, configured next-chat delay, Stop cancellation,
legacy-switch normalization, Standard policy preservation, exactly three visible
Show timing groups and absence of retired controls. Earlier lifecycle/storage/
diagnostics protections remain tested. Synthetic/isolated evidence does not prove
native Windows/live-account acceptance; no real browser/login run is implied.
