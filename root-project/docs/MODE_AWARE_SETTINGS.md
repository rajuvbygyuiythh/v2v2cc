# Mode-aware settings — scoped implementation

Date: 2026-10-18. User-approved workflow: Think → Scope → Edit → Test → Provide.

## Scope and invariant

One common form and one authoritative timing source **after migration**. Browser
choice is not a chat execution mode. Preserve existing Standard worker behavior;
Show retains its three-timing contract and existing send/timeout safety.

Targeted production files:

- `internal/model/ui.go`: explicit mode/read-only effective-settings DTOs.
- `internal/uistore/{chat_settings,store,show_mode}.go`: canonical record,
  migration, legacy API adapters, and Standard browser preference isolation.
- `app.go`, `chat_settings.go`, `show_control.go`: request normalization,
  execution-boundary resolution, save guards, and detached run snapshots.
- Frontend `dist/{index.html,app.js}` and associated Wails bindings: one shared
  form, contextual Standard controls, read-only Details, draft protection.
- Existing targeted Go/Node test files, this scope/evidence note, and project
  map/memory documentation.

No browser driver, login/authentication implementation, proxy, license, AI/EVA,
worker execution logic, live chat timer/send logic, dependency pins, workflows,
or legacy duplicate `cmd/chitchatui/app.go` changes are authorized or needed.
No live browser/account runs, remote publication, or new release archive.

## Execution mapping

| Explicit entry point | Actual driver / engine | Applied settings |
|---|---|---|
| Rod Run | Rod, login capture only | Current launch request; stale `live_chat` cleared |
| ChromeDP Run | ChromeDP, login capture only | Current launch request; stale `live_chat` cleared |
| Show browser Run | Visible Rod; ready only after login | Visible launch preset; no automatic chat Start |
| Start Show Chat | Visible Rod DOM runner | Validated three-timing snapshot, frozen for that run |
| Start Standard Bot | Existing saved-profile workers | Common timings plus Standard first-message/poll/action settings |

A Show launch no longer overwrites Standard driver/profile/headless defaults.
Show launch parameters are transient; the registered browser shows its actual
profile. A historical Show launch may already have overwritten preferences in an
older release; those lost values cannot be reconstructed. Review Standard browser
preferences once if needed. Rod/ChromeDP still share the existing Standard browser
preference form, not duplicated per-driver forms.

## Storage and migration

`settings/chat_canonical/v2` stores the validated `GeneralSettings` value, including
Standard-only policy. Show's runtime settings are derived from it with
`ThreeTimings()`. Common values are saved once, not mirrored into two writable
engine records. The v1 unified marker remains only for compatibility/migration.

- Unreviewed legacy profiles remain distinct migration candidates. The existing
  first-setup selector copies **only** the chosen three timing ranges.
- Consistent already-unified v1 records migrate automatically without changing
  Standard first-message/poll/action settings.
- Divergent v1 common timings return `legacy_conflict`, block both resolvers and
  legacy setters, and require explicit review + common Save. No silent winner.
- After migration, old General/Show records remain backup candidates only;
  reads and compatibility setters use the canonical record. Stale backup writes
  cannot affect effective settings.
- Invalid canonical data returns an error rather than falling back to backups.
- Legacy API compatibility is for the current application. Downgrading to an old
  binary that does not understand v2 is not supported; keep a database backup
  before any manual downgrade.

## UI and API contract

Shared controls, once: Reply delay, Next chat wait, User silent timeout.
Show uses the same finite silence timeout before the first user SMS and during
an ongoing conversation; its normal opener and always-on skip are unchanged.

Standard context additionally exposes first-message policy (bot/user/random,
chance, optional first-wait timeout) and polling/action intervals in its collapsed
Advanced section. These controls are hidden and disabled in Show context.
Switching modes preserves shared timing drafts; unsaved Standard-only edits must
be saved before switching to Show so a Show Save cannot silently discard them.

`SaveChatSettings.edit_mode = standard` explicitly enables Standard-only edits.
`show` preserves all stored Standard-only values. Omitted mode retains the prior
API contract: it may edit polling/action intervals but never first-message
policy. Unknown edit modes are rejected. Legacy General/Show setters remain available, with idle/drain
save guards and canonical validation. All settings Saves are blocked while either
mode runs or account ownership drains.

`GetExecutionSettings(standard|show|login)` returns a redacted, read-only
mode/driver/source/revision projection. Preview labels distinguish next Start
from a frozen Standard run; each Show browser's Run details retain its own frozen
settings and revision. Returned pointer-bearing views are detached copies.
No credentials, message text, tokens, or account data are added to this DTO.

## Verification procedure and limits

Only browser-free synthetic tests and temporary databases/profiles are used.
The scope's final evidence is recorded below and in the dated project-memory
entry. Existing reply delay, first/ongoing silence, pending replies/follow-ups,
unconfirmed-send pause, cancellation and ownership regression tests remain intact.

Environment preparation is outside the repository. Go 1.25.0 was bootstrapped
from Go sources fetched from GitHub. Dependency archives were reconstructed with
Go's module-zip tooling from pinned GitHub sources and checked against the
unchanged production `go.sum`. No production replacements or dependency edits.
The sourcehut-only go-toast dependency is unavailable on the network allowlist.

The root package imports Windows-only WMI. Synthetic Linux root tests/vet use an
**external modfile** replacing only WMI with a shim that returns an unsupported
error. It does not implement authentication, is not shipped, and is never used
for Windows compilation. Report this separately from production-pin checks.
Native Windows/Wails installation, browser launch, login and real conversations
remain NOT_RUN without explicit authorization. Cross-compilation is not native
runtime acceptance.

### Final results

| Check | Result | Qualification |
|---|---|---|
| `npm test` | PASS, 90/90 | Current frontend, contracts, package/map and DOM tests |
| Production-pin internal regression with `-race` | PASS | Eight tested packages; store/API also compile (no test files) |
| Production-pin internal `go vet` | PASS | Same ten package targets |
| Linux synthetic root `-race` + `go vet` | PASS | External WMI error shim/modfile only; not native certification |
| `GOOS=windows CGO_ENABLED=0 go build ./...` | PASS | Production pins, no replacements, default build tags |
| Windows root test executable compilation | PASS | Production pins, compiled only, not executed |
| Windows `-tags production` build | BLOCKED | Wails imports sourcehut-only go-toast; dependency unavailable on this network |
| Native Windows/Wails build + execution | NOT_RUN | Windows environment required |
| Live browser/account/chat acceptance | NOT_RUN | Not authorized |

All 57 available pinned dependency archives were reconstructed from GitHub and
accepted by Go with the unchanged production checksum file. The remaining
sourcehut-only dependency was not substituted. `GOSUMDB=off` prevents calls to
unreachable checksum servers; existing `go.sum` verification remains active.

`npm run verify` reports **BLOCKED**: frontend syntax/contract tests, Go version,
formatting and source-stability checks pass, but the complete dependency-download
step cannot reach the required dependency server. Its downstream Go/native checks
are marked BLOCKED, not PASS; supplemental passing checks above are separate.
The machine-readable gate status is in `verification-output/report.json`. No claim of complete
live/native operation and no new verified release archive is made.
