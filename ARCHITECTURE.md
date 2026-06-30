# CLI Architecture

## Overview

This CLI is built around two strictly separated layers. The boundary between them is a logical necessity, not a style choice.

```
┌─────────────────────────────────────────────┐
│  Layer 2: Presentation Layer                │  ← Serves LLM + terminal constraints
│  Overflow | Metadata footer | stderr attach  │
├─────────────────────────────────────────────┤
│  Layer 1: Execution Layer                   │  ← Pure command semantics
│  Command routing | API calls | raw output   │
└─────────────────────────────────────────────┘
```

---

## Why Two Layers

This CLI serves two primary callers:

1. **LLM agents** — need progressive disclosure (overflow), structural signals (footer), and self-navigating output (drillable refs, pagination-as-commands).
2. **Scripts and programs** — need stable, parseable structured output (`--json` NDJSON mode).

Both share a constraint: **Layer 1 must produce raw, lossless output.** If you truncate a response mid-processing, you break composition. If you annotate it with footers inside execution code, you corrupt the JSON stream. The only correct position for presentation transforms is **after** execution completes.

The LLM caller drives the need for Layer 2 (overflow, footer, stderr attachment). The script caller drives the need for `--json` to bypass Layer 2 entirely. Both depend on Layer 1 being pure.

The `--pretty` mode (human-readable ANSI output) is a third caller type. It also belongs in Layer 2 — or as a rendering path inside Layer 1 that returns an ANSI string — never as logic mixed into the API call itself.

---

## Layer 1: Execution

**Responsibility:** Call the API. Return raw results.

- Routes subcommands to API calls
- Handles authentication (loads credentials from Keychain)
- Captures full API responses — no truncation, no annotation
- Returns raw output as `(string, error)`

Layer 1 has no knowledge of output limits, terminal width, or ANSI codes (except for `--pretty` rendering paths that return formatted strings). It executes and returns.

**Files:**
- `internal/` — API client packages
- `cmd/` — subcommand routing and flag parsing

---

## Layer 2: Presentation

**Responsibility:** Transform Layer 1 output for safe, efficient consumption.

Applied after execution completes. Never touches execution logic.

### Mechanism A: Overflow (Progressive Disclosure)

This is the core mechanism that makes LLM consumption safe. Without it, a single large API response can blow out the agent's context window and degrade all subsequent reasoning.

**Trigger:** output exceeds 200 lines OR 50 KB (whichever fires first).

**Behaviour:**

1. Truncate to the first 200 lines (rune-safe split — no broken UTF-8 mid-character)
2. Write the **complete, unmodified** output to `/tmp/jiracli-output/output-N.txt`
3. Append an overflow notice with ready-to-run exploration commands

```
[first 200 lines of output, verbatim]

--- output truncated (1420 lines, 89.4KB) ---
Full output: /tmp/jiracli-output/output-3.txt
Explore:     cat /tmp/jiracli-output/output-3.txt | grep <pattern>
             cat /tmp/jiracli-output/output-3.txt | tail 100
Narrow:      jiracli <command> --help
```

**Why this works:** The agent already knows `grep`, `head`, `tail`, `wc`. Overflow converts a context-budget problem into a navigation skill the agent already has. The full data is never lost — it's one `cat` away. The agent can:
- `grep` for a keyword to find relevant lines
- `tail` to see the end of the output
- `head -n 50` a section after finding a line number
- Re-run the command with narrower flags to reduce output at the source

**Implementation:** `internal/output/presenter.go` — the `overflow()` function. The temp directory uses the tool name (`/tmp/jiracli-output/`) and a monotonically increasing counter for unique file names within a process lifetime. Files are world-readable (`0600`) and persist until system reboot or explicit cleanup. ANSI escape sequences are stripped from the file content so `grep`, `cat`, and LLM tools work cleanly on it — the displayed (truncated) lines retain their ANSI for terminal rendering.

**Not applied in JSON mode.** When `--json` is set, overflow is bypassed entirely. Scripts handle their own pagination and memory management.

### Mechanism B: Metadata Footer

After execution, append to every response:

```
[exit:0 | 1.2s]
```

- Exit code using Unix convention (0 = success, non-zero = failure)
- Duration in human-readable form

The footer is **always present**, including on success. The agent internalises these signals over a conversation. Inconsistent output format means every call feels like the first.

The footer is suppressed in JSON mode (`--json`) because it would corrupt the NDJSON stream.

### Mechanism C: stderr Attachment

On any non-zero exit:

```
[stdout content if any]
[stderr] reason for failure here
[exit:1 | 3ms]
```

**Never drop stderr.** The most common mistake is discarding stderr when stdout has content. This is catastrophically wrong for agents: the agent receives "it failed" with no information about why, and retries blindly.

Errors must be self-contained and corrective. Include the exact command the agent should run to recover:

```
[stderr] credentials not found for profile "prod" — run: jiracli auth login
[exit:1 | 2ms]
```

### Mechanism D: Help on Error

When a wrapped command's `RunE` returns a non-nil error, `WrapWithPresenter` emits the command's help text before the error block:

```
Usage:
  jiracli hello [flags]

Flags:
  --profile string   Profile name
  --json             Output NDJSON
  ...

[stderr] credentials not found for profile "prod" — run: jiracli auth login
[exit:1 | 2ms]
```

No-arg or bad-arg invocations are always self-documenting. The caller never needs to separately invoke `--help` to understand what went wrong.

---

## Output Modes

Every command that produces structured records supports two output modes:

| Mode | Flag | Format | Audience |
|---|---|---|---|
| Plain text | (default) | Human-readable text + `[exit:N]` footer | LLM agents |
| NDJSON | `--json` | One JSON object per line, no footer | Scripts, programs |

`jiracli` ships plain text and `--json` only. There is no `--pretty` mode.

### Plain Text (default)

The primary output format. Designed for LLM agents: parseable with `grep`, `awk`, `head`. The footer is always present. Overflow is applied automatically.

### JSON Mode (`--json`)

**Layer 2 is bypassed entirely.** When `--json` is set, `WrapWithPresenter` writes the raw buffer directly to stdout without footer, overflow, or stderr attachment. The footer would corrupt the NDJSON stream.

Rules:

- **One JSON object per line.** No top-level array, no envelope. Each logical record is emitted as a single compact JSON object followed by a newline (NDJSON convention). `wc -l` counts records; `grep` filters them; `jq -c '.'` validates.
- **Errors go to stderr only, exit non-zero.** In JSON mode, error messages are written to stderr as plain text. stdout may be empty or contain partial NDJSON if an error occurs mid-stream. No JSON error object is written to stdout — the stream must remain parseable.
- **No auto-pagination.** Callers must page explicitly. There is no `--all` flag that silently fetches all pages — the caller controls its own memory budget.
- **Output stability is a contract.** Within a version series, `--json` field names and types are stable. Adding new fields is allowed (callers must tolerate unknown keys). Removing, renaming, or changing a field's type is a breaking change.

#### JSON Pagination

When a command supports pagination and more results exist, the **last line** of stdout is a pagination trailer object:

```json
{"_pagination": {"next_page": 2, "has_more": true, "total": 47, "page": 1, "pages": 3}}
```

Contract:

- The trailer is always the final line. Data records precede it.
- The leading underscore on `_pagination` makes it unambiguously not a data record. Consumers detect it with `jq 'select(._pagination)'` or a prefix check.
- **No trailer is emitted on the last page.** If the trailer is absent, there are no more results.
- The trailer fields depend on the pagination style:

| Style | Fields | How to fetch next page |
|---|---|---|
| Page-based | `next_page`, `has_more`, `total`, `page`, `pages` | Pass `--page <next_page>` |
| Cursor-based | `has_more`, `next_cursor` | Pass `--cursor <next_cursor>` |

- Both styles are valid. Choose based on what the upstream API provides.
- `has_more` is always present and always a boolean. It's the canonical "should I keep paging?" signal.

Example consumer (shell):
```bash
# Fetch all pages into a single file, stripping pagination trailers
page=1
while true; do
  jiracli search --json --page "$page" "query" > /tmp/page.json
  grep -v '^{"_pagination"' /tmp/page.json >> results.json
  next=$(jq -r 'select(._pagination) | ._pagination.next_page // empty' /tmp/page.json)
  [ -z "$next" ] && break
  page="$next"
done
```

Example consumer (Go):
```go
// Detect trailer: last line starts with {"_pagination"
lines := strings.Split(strings.TrimSpace(stdout), "\n")
last := lines[len(lines)-1]
if strings.HasPrefix(last, `{"_pagination"`) {
    var trailer struct { P struct { NextPage int `json:"next_page"` } `json:"_pagination"` }
    json.Unmarshal([]byte(last), &trailer)
    // trailer.P.NextPage is the next --page value
}
```
---

## Authentication and Credentials

Credentials are stored in the macOS Keychain via `internal/keychain`. One generic-password item per named profile.

All commands that require credentials:
1. Read the `--profile` flag (or equivalent)
2. If empty, call `keychain.ResolveDefault()`
3. Call `keychain.Load(profile)` to retrieve the entry
4. Pass the credentials to the API client

The `--profile` flag (or whatever your credential selector is named) is **always optional** on every command. Never use `MarkFlagRequired` on it. Resolve via `keychain.ResolveDefault()` when absent: a single saved profile wins implicitly; multiple saved profiles require an explicit flag.

Resolution order for `ResolveDefault`:
1. Stored default (`keychain.GetDefault`)
2. Single saved profile (implicit)
3. Error — ambiguous or empty

---

## Package Structure

```
jiracli/
├── main.go                    Entry point: run(args, stdout, stderr); Layer 1→2 bridge
├── main_test.go               Tests for run() and top-level routing
├── cmd/
│   ├── errors.go              Sentinel errors (ErrAlreadyPresented)
│   ├── prompt.go              Shared stdin/stderr prompt helpers
│   ├── write.go               HandleWrite lifecycle (preview → confirm → execute)
│   │
│   ├── setup.go               Interactive auth + skill wizard
│   ├── auth.go                auth login / reauth / logout / profile / status
│   ├── cache.go               cache list / cache clear
│   ├── config.go              config command group
│   ├── config_hierarchy.go    config hierarchy: view/update per-profile hierarchy field IDs
│   ├── config_agile.go        config agile: view/update sprint custom-field ID
│   │
│   ├── show.go                show <ref> root; dispatches to sub-commands
│   ├── issue.go               show <KEY>: fetch + render one issue
│   ├── assigned.go            show assigned: issues assigned to current user
│   ├── comments.go            show comments <KEY>: paginated comment thread
│   ├── history.go             show history <KEY>: paginated changelog
│   ├── transitions.go         show transitions <KEY>: available workflow transitions
│   ├── show_hierarchy.go      show hierarchy <KEY>: walk Initiative → Epic → Subject → Children
│   ├── attachments.go         show attachments <KEY>: list attachments
│   ├── attachment.go          attachment download helper (streaming to stdout or file)
│   │
│   ├── search.go              search <jql>: JQL search with pagination
│   ├── open.go                open <ref>: open issue/comment/attachment in browser
│   ├── me.go                  auth status (current user)
│   │
│   ├── add.go                 add comment / link / attachment (group)
│   ├── comment.go             add comment <KEY>
│   ├── link.go                add link <source> <target>
│   ├── attach.go              add attachment <KEY> <file…>
│   │
│   ├── delete.go              delete <ref>: dispatches by ref shape (issue/comment/attach/link); aliased as "rm"
│   ├── delete_comment.go      DeleteComment Layer 1 func (no cobra command)
│   ├── delete_attachment.go   DeleteAttachment Layer 1 func (no cobra command)
│   ├── delete_link.go         DeleteLink Layer 1 func — accepts KEY:link:ID ref
│   ├── delete_issue.go        DeleteIssue Layer 1 func (no cobra command) — --with-subtasks flag
│   │
│   ├── edit.go                edit group (status / assignee / field)
│   ├── transition.go          edit status <KEY> <name-or-id>
│   ├── assign.go              edit assignee <KEY> <user-or-id>
│   ├── field.go               edit field <KEY> <spec…>
│   ├── edit_sprint.go         edit sprint <KEY...> <target>: move issues into sprint or backlog
│   │
│   ├── create.go              create issue
│   ├── rollup.go              show rollup <KEY>: aggregate time + SP across hierarchy levels (--depth, --list, --all)
│   ├── credentials.go         resolveEntry / resolveEntryAndStore — shared credential helpers
│   ├── lookup.go              lookup command group root + suggestLabels helper
│   ├── lookup_users.go        lookup users
│   ├── lookup_labels.go       lookup labels
│   ├── lookup_components.go   lookup components
│   ├── lookup_versions.go     lookup versions
│   ├── lookup_projects.go     lookup projects
│   ├── lookup_issue_types.go  lookup issue-types
│   ├── lookup_link_types.go   lookup link-types
│   ├── lookup_statuses.go     lookup statuses
│   ├── lookup_priorities.go   lookup priorities
│   ├── lookup_fields.go       lookup fields
│   ├── lookup_boards.go       lookup boards: list Agile boards for a project
│   ├── board.go               board list/show/issues: Agile board inspection
│   ├── sprint.go              sprint list/show/issues/current: Agile sprint inspection
│   └── skill.go               skill install/uninstall helpers
├── internal/
│   ├── keychain/
│   │   └── keychain.go        macOS Keychain: save/load/delete/list/default credentials; Entry.HierarchyConfig, Entry.AgileConfig
│   └── output/
│       └── presenter.go       Layer 2: overflow, footer, stderr attachment
└── ARCHITECTURE.md
```

### Entry point contract

```go
func main() {
    if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
        os.Exit(1)
    }
}

func run(args []string, stdout, stderr io.Writer) error { … }
```

`run` takes explicit I/O writers. Tests pass `bytes.Buffer`; production passes `os.Stdout`/`os.Stderr`. No I/O is hardcoded below `main()`.

### Layer 1 → Layer 2 bridge

`cmd/` functions return `(string, error)` — raw output string and execution error. `main.go`'s `WrapWithPresenter` captures the output into a buffer, measures elapsed time, and calls `output.Format` before writing to `stdout`.

When `--json` or `--keys-only` is set on the command, `WrapWithPresenter` bypasses `output.Format` entirely and writes the buffer verbatim — no footer, no overflow. Both flags produce machine-readable output that the footer would corrupt or pollute.

---

## Cobra Wiring Rules

- Never set `RunE` on a command group. Bare group invocation must show help.
- `SilenceUsage: true` and `SilenceErrors: true` on root. Errors are printed through the presenter, not Cobra's default handler.
- `MarkFlagRequired` must be called on the exact command that owns the flag, not on parent groups.
- A `RunE` that writes its own presenter output (streaming commands) must return `errAlreadyPresented`.
- Never call `os.Exit` inside `RunE`. It makes the code path untestable and violates the `run()` contract.

### Streaming commands

Commands that stream output (WebSocket events, long polls, live tails) cannot use `WrapWithPresenter` because there is no single return point to wrap. They:

1. Write events directly to `stdout` as they arrive
2. Apply the footer once at exit via `output.Format`
3. Return `errAlreadyPresented` so `run()` does not emit a second footer

In JSON mode, streaming commands suppress the footer entirely and write errors to stderr only.

---

## Design Constraints

**Layer 1 must be raw and lossless.** Do not truncate, annotate, or transform output inside execution code. Pass the full result up.

**Layer 2 must not call APIs.** Presentation logic has no business making network calls. If you find yourself needing to fetch additional data in the presenter, it belongs in a Layer 1 command.

**Output must be pipeable.** Every command's stdout must survive `| grep`, `| jq`, `| head`. The metadata footer uses bracket syntax (`[exit:0]`) that is unlikely to appear as data content and can be stripped with `grep -v '^\[exit:'` if needed.

**Commands are not interactive.** No `readline`, no spinners on stdout, no "press enter to continue." The primary caller is a program running in a loop.

**Errors are corrective, not descriptive.** Every error message tells the caller exactly what to run next. "credentials not found" is not an error message. "credentials not found for profile "prod" — run: jiracli auth login" is.


**Output is self-navigating.** Every result that references a deeper resource includes the exact command to fetch it. The agent never needs to construct references from raw IDs — the output hands them ready-to-run. For example, a search result includes `→ jiracli read <channel>:<ts>` on each match so the agent can drill into any thread without prior knowledge of the reference format.

**Pagination footers are runnable commands.** In plain-text mode, when more pages exist the footer emits the complete next-page command with all active flags reconstructed. The agent copy-pastes it directly — no flag reconstruction, no manual cursor management:
```
--- page 1 of 3 | next: jiracli search --page 2 --count 20 --channel general "deploy" ---
```
In JSON mode this is expressed as a `{"_pagination": {...}}` trailer object instead.
**JSON output stability is a versioned contract.** Field names and types do not change within a version series. New fields may be added; callers must tolerate unknown keys.

---

## Jira-specific entity model

All Jira types live in `internal/jira/`. The two-layer architecture applies
unchanged — `internal/jira/` is Layer 1 execution; `internal/output/` is
Layer 2 presentation.

### Package map

| File | Responsibility |
|---|---|
| `client.go` | HTTP client (Get/Post/Put/Delete/PostMultipart), `MapStatus`, `rateLimitError`, sentinel errors (`ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`, `ErrRateLimited`, `ErrServer`) |
| `agile.go` | Agile REST client (`AgileGet`/`AgilePost`/`AgilePut`, `agileURL`), domain types (`Board`, `BoardConfig`, `BoardColumn`, `BoardFilter`, `Sprint`, `AgileConfig`), read methods (`ListBoards`, `ListBoardsCached`, `GetBoardConfig`, `GetBoardConfigCached`, `GetBoardFilter`, `ListSprints`, `ListSprintsCached`, `ListSprintNames`, `HydrateSprintDates`, `ListAllSprintsPaged`, `GetSprint`, `ListSprintIssues`, `ListBoardIssues`), mutation methods (`MoveIssuesToSprint`, `MoveIssuesToBacklog`), `ResolveSprintField`, `ErrBoardNoSprints`, `sprintQueryEnvelope` |
| `ref.go` | Reference grammar parser (`ACME-123`, `:comment:`, `:attach:`, `:link:`, browse URLs) — `RefIssue`, `RefComment`, `RefAttachment`, `RefLink` |
| `issue.go` | GetIssue, DeleteIssue, IssueRaw, IssueRecord (incl. `portfolio`), HierarchyFieldIDs, ExtractRawKey, ToIssueRecord, ResolveActivityStatusCategories, SprintRef, parseSprintRaw |
| `search.go` | Search (POST /search), SearchIssueRecord (incl. Sprints []SprintRef), ToSearchRecord |
| `jql.go` | DefaultOpenFilter (statusCategory-based, not status names) |
| `comments.go` | GetComments, AddComment |
| `history.go` | GetChangelog (DC 8.7+ dedicated endpoint; falls back to expand=changelog) |
| `transitions.go` | GetTransitions, DoTransition |
| `attachments.go` | GetAttachmentMeta, DownloadAttachment, UploadAttachment |
| `lookup.go` | TTL constants, shared types (Project, Component, Version, Priority, ...), list methods, `PortfolioCandidates`, TTLBoards, TTLBoardConfig, TTLSprintsActive, TTLSprintsClosed |
| `projects.go` | ListProjects, GetProject, ListProjectIssueTypes, GetCreateMeta, ListProjectPriorities |
| `users.go` | SearchUsers, SearchAssignableUsers, Assign |
| `fields.go` | ResolveFieldID, UpdateFields |
| `fieldspec.go` | ParseFieldSpec, FieldOp (=, +=, -=) |
| `links.go` | CreateLink, DeleteLink (via `DELETE /issueLink/:id`), AggregateLabelsByProject |
| `create.go` | CreateIssue |
| `preview.go` | Preview, ValidationRow, Render, Execute — supports `Method: "DELETE"` (body block suppressed when nil) |
| `render.go` | FormatRelative, WrapAt, FormatBytes, TruncateString, ColWidth, PadRight, IsASCIILetter, RenderWikiMarkup, AbbreviateChange, StatusCategoryRank, CommonRunePrefix, TruncateMidPrefix |
| `badges.go` | `ColorsEnabled`, `StripAnsi`, `ColorIssueType`, `ColorStatusName`, `Bold`, `Dim` — ANSI badge helpers used by renderers and `internal/output` |
| `hierarchy.go` | `HierarchyNode`, `HierarchyChain` (incl. `DescendantsTruncated`), `BuildHierarchy` (depth + since params), `fetchChildrenForParents`, `strategyForLevel`, `parentKeyForChild` — ancestor walk (Portfolio → ParentLink → Parent → EpicLink), batched multi-level children fetch, 400 batch-halving retry |
| `hierarchy_render.go` | `RenderHierarchy` — colored/plain tree renderer with recursive subtree (depth ≥ 2), `renderChildSubtree`; `RenderHierarchyFlat` — tab-separated flat output with DFS order and negative-depth ancestors |
| `rollup.go` | `RollupRow`, `RollupNode`, `RollupTree`, `ChildJQL`, `AggregateNodes`, `RollupNodeFromRaw`, `SubjectRowFromRaw`, `IssueTypeHasEpicLinkChildren` — hierarchy time/SP rollup types and aggregation helpers |

### HTTP error handling

`do()` (the shared request executor in `client.go`) converts HTTP error responses before returning to callers:

| Status | Sentinel | Message shape |
|---|---|---|
| 401 | `ErrUnauthorized` | Names the profile and gives the `auth reauth` command |
| 403 | `ErrForbidden` | Access denied |
| 404 | `ErrNotFound` | Surfaces Jira's own `errorMessages` when present |
| 429 | `ErrRateLimited` | Handled directly in `do()` before the body is passed to `MapStatus`; reads `retry-after`, `X-RateLimit-Remaining`, and `X-RateLimit-Limit` response headers to produce a plain-English message telling the user how long to wait |
| 5xx | `ErrServer` | Includes the status code |
| 400 | _(unwrapped)_ | Surfaces Jira's validation/JQL error messages |

Rate limiting (429) is intercepted in `do()` rather than `MapStatus` because the wait time lives in response headers, which `MapStatus` never sees. All other status mappings happen in `MapStatus` after the body is read.

### Cache layout

```
~/.cache/jiracli/<sha256[:4](profile+"\x00"+url)>/
  myself.json             # TTL 24h
  fields.json             # TTL 24h
  projects.json           # TTL 24h
  statuses.json           # TTL 7 days
  issuetypes.json         # TTL 24h
  linktypes.json          # TTL 24h
  priorities.json         # TTL 24h
  project/<KEY>.json      # TTL 1h
  project/<KEY>/
    priorityscheme.json   # TTL 24h
  issuetypes/<KEY>.json   # TTL 24h
  createmeta/<KEY>/
    <typeID>.json         # TTL 24h
  labels/<KEY>.json       # TTL 5m (JQL aggregation)
  boards/<KEY>.json       # TTL 1h  (page 1, limit ≥ 50 only)
  board/<id>/
    config.json           # TTL 1h
  sprints/<boardID>/
    active+future.json    # TTL 1h  (agile/1.0 page 1, default states)
    closed.json           # TTL 7d  (agile/1.0 page 1, --state closed)
    names.json            # TTL 1h  (GreenHopper fast path: all sprint names+states in one call)
    closed-all.json       # TTL 7d  (full paged fetch of all closed sprints, for filter flags)
```

The hash namespaces caches per (profile, URL) so switching instances never
serves stale data from a different instance.

`--no-cache` on any command bypasses reads and writes for that invocation.
`jiracli cache clear [--key <glob>]` removes entries manually.
