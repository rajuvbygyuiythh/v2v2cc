# First-message policy — 2026-10-17

Owner-authorized scope: add first-sender controls to the two existing timing
profiles. No browser driver, login, proxy, license, AI or EVA engine changes.

## Controls (both profiles, saved independently)

- **Bot first** (legacy default): try an opener unless a partner message is
  already detected; that incoming message instead receives the first reply.
- **User first**: do not send an unsolicited opener.
- **Random**: choose once per new conversation. Bot-first chance is 0–100%:
  0 = always user first, 100 = always bot first. Other values are independent
  probabilities, not fixed conversation quotas. The editable default is 70%.
- **First-message wait**: indefinitely (default), or skip after a random positive
  min/max timeout. This applies to user-first decisions, including Random's
  user-first branch. It is separate from no-reply timeout after a bot send.

Save then restart the corresponding runner. Existing serialized settings with
no policy remain bot-first. No existing timing fields are migrated between
profiles. `GeneralSettings.first_message` and `ShowModeSettings.first_message`
are independent nested values under their existing separate storage keys.

## Runtime boundaries

Show Mode chooses when constructing the responder for a conversation. It keeps
that choice while waiting and resets first-wait state on a new conversation.
A first-message timeout uses the existing safe DOM Skip/START path, including
next-chat wait. No-reply Auto Skip can remain OFF; first-message timeout is its
own explicit option. Both timeout paths yield to detected partner messages.
Own/unknown rows in the initial snapshot are baseline history; partner rows
already present are now handled as incoming rather than silently discarded.
A browser resumed into an old active conversation therefore may reply to a
retained partner row; no claim is made that all such rows are newly received.
Reply delay applies to first replies, not unsolicited openers. Sequence replies
lazily initialize with line 1; EVA receives the first incoming in stage NEW.

Standard workers persist `FirstTurns` by account + conversation in the existing
private bot-state JSON. Random choice and first-wait deadline survive polling
and restart; changing settings does not reroll an existing stored conversation.
A match first-turn gate checks API history and queued realtime events before
opening, then waits for partner input/close/cancellation or the first deadline.
An additional history check at timeout lets already-arrived input win. History
check errors fail closed rather than sending a guessed opener. Captured initial
input is passed once to the existing sequence/EVA/legacy-keyword responder;
a duplicate realtime event for that same ID is ignored by later match loops.
Existing send counts and sequence assignment semantics are retained.

DM first-message paths (poll and realtime) honor the same conversation decision:
user-first DMs are left idle until incoming input arrives. **Match timeout skip
is not DM deletion/disconnection**; an idle DM is skipped for that processing
pass, remains in the inbox and can be answered later. No destructive DM action
was added. No server-atomic ordering guarantee is possible: input can arrive
after the final history/DOM check and before sending. Existing subsequent
worker reply loops are not redesigned by this feature.

## Targeted implementation files

- `internal/model/first_message.go`, `ui.go`, `model.go`: policy and stored choice.
- `internal/uistore/{store,show_mode}.go`: validation at both Save boundaries.
- `app.go`, `internal/bot/run.go`: standard profile → worker config only.
- `internal/bot/first_message.go`: persisted choice and first-match gate.
- `internal/bot/logic.go`: first-DM branches, first-match input bridges and dedupe.
- `live_chat.go`: first-turn DOM wait and lazy sequence start.
- Dashboard assets and checked-in model bindings: two separate sets of controls.
- Focused regression tests, format gate and project docs.

## Evidence / limitations

- Actual frontend: 72 Node tests PASS, including policy visibility, save
  separation, defaults and validation.
- Original model package: Go tests PASS without third-party dependencies.
- Temporary source copy: full model/bot/uistore Go race tests PASS, including
  persisted choice/reload, first wait/cancel/close, late incoming at timeout,
  Single/Folder line-1 and realtime duplicate handling, DM poll/realtime
  suppression, EVA first-input underage guard, and settings reopen/isolation.
- Production App/Show Mode/transcript/browser-runner source and selected tests
  executed with race detection PASS in that temporary copy. Only the Windows
  WMI hardware package was replaced with a panic-on-access test boundary;
  no hardware/license access occurred, and production license source is unchanged.
- Temporary-copy Windows cross-build and scoped Go vet PASS. Temporary go.mod
  uses pinned GitHub mirrors for otherwise inaccessible vanity module hosts.
  Repository go.mod/go.sum are unchanged. These isolated checks do NOT equal
  full standard-gate verification of the original module/dependency environment.
- Native Windows/Wails runtime, real browser login/site acceptance: NOT_RUN.

Manual acceptance: test each mode's Bot/User/Random policy independently;
verify 0%/100%, incoming before opener, first wait timeout vs unlimited,
no-reply skip after a reply, line 1, age guard, multiple browsers, partner
changes and restart persistence. Use synthetic data in automated tests only.
