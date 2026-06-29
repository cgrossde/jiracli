# jiracli — Read Commands

Reference for: `issue`, `search`, `assigned`, `comments`, `history`, `transitions`, `attachments`, `attachment download`, `open`.

All read commands accept `--profile <name>` and `--json`. Paginated commands accept `--limit` and `--page`.

---

## Reference grammar

| Form | Meaning |
|---|---|
| `ACME-123` | Issue key |
| `ACME-123:comment:NNN` | Specific comment (accepted by `open` only) |
| `ACME-123:attach:NNN` | Specific attachment |
| `ACME-123:link:NNN` | Specific issue link (accepted by `delete` only) |
| `https://<host>/browse/ACME-123` | Full browse URL (accepted anywhere a key is) |

---

## `issue <KEY>`

Fetches a single issue with inline latest comment and changelog.

    jiracli show <KEY> [flags]

`<KEY>` accepts a bare key or a full browse URL.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--comments N` | 1 | Preview last N comments inline (0 = none, max 25) |
| `--no-comments` | false | Omit comments section entirely |
| `--no-history` | false | Omit activity/changelog section |
| `--no-children` | false | Skip the children list (one fewer API call) |
| `--parent` | false | Show this issue's parent instead (Parent Link → Parent → Epic Link) |
| `--fields <spec>` | — | Override field set (see below) |
| `--json` | false | NDJSON output |
| `--profile <name>` | default | Credential profile |

### API call

`GET /rest/api/2/issue/<KEY>?fields=key,summary,status,assignee,reporter,description,labels,components,priority,issuetype,created,updated,comment,fixVersions,parent,issuelinks,attachment,resolution&expand=changelog`

Never uses `fields=*all` or `expand=renderedFields`.

When a hierarchy configuration exists for the profile (set via `setup` or `config hierarchy`), the Epic Link and Portfolio custom-field IDs are appended to the field list automatically.

### `--fields` spec

- **Add to default:** `--fields "description,reporter"` — adds those fields to the default set. (`+description` is accepted as an alias.)
- **Drop from default:** `--fields "-assignee,-priority"`.
- **Mixed:** `--fields "timetracking,-priority"`.
- **Restrict to a fixed set:** `--fields-only "key,summary,description"` — fetches and renders exactly those fields. Mutually exclusive with `--fields`.

Field names match Jira's own field IDs as returned by `jiracli lookup fields`. Custom fields accepted by name (`team`) or id (`customfield_10031`).

### Plain-text output shape

```
ACME-123  In Progress · Bug · High
"Summary text"

Assignee: Alex Chen (u1)               Reporter: Sam Patel
Created:  2026-05-10                   Updated: 2026-06-22

Epic: ACME-100  "Epic summary"
Portfolio: ACME-50  "Modernise authentication platform"  (Open)

Fix Versions: 2026.06
Labels: label1, label2

Description:
  Body wrapped at 80 cols, 2-space indent.

Links (2):
  blocks              ACME-145            In Progress     Summary text                                        (id: 10234)
  relates to          ACME-201            Done            Summary text                                        (id: 10235)
  → jiracli delete ACME-123:link:<id>
  → jiracli add link ACME-123 OTHER-123 --type "is related to"

Components: ComponentName

Attachments (1):
  [1] filename.ext  142 KB  2026-05-11  (id: ACME-123:attach:11001)
  → jiracli show ACME-123:attach:11001

Children (3 of 5 shown):
  ACME-126      To Do         Bug       Sam Patel             "Child three"
  ACME-124      In Progress   Story     Alex Chen             "Child one"
  ACME-125      Done          Task      __Unassigned          "Child two"
  → jiracli search "parent = ACME-123"

Latest comment (N of TOTAL):
  — Alex Chen (u1)  2026-06-22
  > Comment body indented 2 spaces.

  → jiracli show comments ACME-123          # full thread (TOTAL comments)

Activity (newest 10):
  2026-05-20 10:15  Alex Chen             description: updated, fixVersion: 2026.05 → 2026.06
  2026-05-11 09:00  Sam Patel             status: Open → In Progress
  …
  (showing 10 newest of 42 entries — jiracli show history ACME-123 for full log)

Drill in:
  → jiracli show comments     ACME-123
  → jiracli show history      ACME-123
  → jiracli show transitions  ACME-123
  → jiracli show hierarchy    ACME-123

[exit:0 | Xms]
```

Activity rules:
- Always shows the **newest 10** entries from the changelog (never more).
- `description` changes always render as `description: updated` / `description: set` / `description: cleared` (body never shown).
- `Comment` changes where both sides are empty (the common case — Jira changelog omits comment bodies) render as `Comment: added`. When bodies are present, they are truncated to 120 chars per side. `environment`, `summary` changes are truncated to 120 chars per side.
- All other fields (status, assignee, fix version, labels, etc.) are shown in full.
- Status regressions (moves to a lower `statusCategory`) are marked with `↩`.
- `Rank_` field changes are always suppressed.
- Use `jiracli show history <KEY>` for the full paginated log.

Children section:
- Sub-tasks are fetched inline from the issue response (no extra API call).
- Epic children (`Epic Link = <KEY>`) require one extra `search` call; use `--no-children` to skip it.
- Use `jiracli show hierarchy <KEY>` to walk the full Initiative → Epic → Subject chain.
- Up to **15 children** are shown: non-Done first, Done last (stable sort within each group).
- When truncated, the heading reads `Children (15 of N shown)` and a `→ jiracli search` hint shows all.
- `Children: (none)` is shown explicitly when no children exist and `--no-children` is not set.
- JSON output always includes `"children": []`, `"childrenTotal": 0`, and optionally `"childrenError"`.

Comment section behavior:
- `total = 0`: section omitted.
- `total > 1`: drill-down hint appended: `→ jiracli show comments ACME-123   # full thread (N comments)`.
- `total > 50`: hint becomes `→ 50+ comments — use jiracli show comments ACME-123 --page 1`.
- `--comments N > 25`: rejected with `[stderr] --comments max is 25 — use jiracli show comments <KEY> --limit <N> for a longer thread`.

### NDJSON output (`--json`)

Single object (v1 schema, additive-only):

```json
{
  "key": "ACME-123",
  "summary": "...",
  "status": "In Progress",
  "statusCategory": "In Progress",
  "resolution": null,
  "priority": "High",
  "issueType": "Bug",
  "assignee": {"name": "u1", "displayName": "Alex Chen"},
  "reporter": {"name": "u2", "displayName": "Sam Patel"},
  "created": "2026-05-10T08:00:00.000+0000",
  "updated": "2026-06-22T08:14:30.000+0000",
  "description": "...",
  "labels": ["perf", "auth"],
  "components": ["AuthService"],
  "fixVersions": [],
  "parent": null,
  "epic": {"key": "ACME-100", "summary": "Auth reliability"},
  "portfolio":      { "key": "ACME-50", "summary": "Modernise authentication platform", "status": "Open", "statusCategory": "To Do" },
  "links": [{"type":"Blocks","direction":"outward","relationship":"blocks","issue":{"key":"ACME-145","summary":"...","status":"In Progress","statusCategory":"In Progress"}}],
  "attachments": [{"id":"11001","filename":"trace.har","mimeType":"application/json","size":145000,"uploaded":"...","author":"u1"}],
  "comments": {
    "total": 5,
    "truncated": false,
    "items": [{"id":"9421","author":{"name":"u1","displayName":"Alex Chen"},"created":"...","body":"..."}]
  },
  "historyTruncated": false,
  "historyTotal": 8,
  "activityTimeline": [{"type":"transition","author":{"name":"u2","displayName":"Sam Patel"},"created":"...","changes":[{"field":"status","from":"Open","to":"In Progress"}]}],
  "children": [{"key":"ACME-124","summary":"Child one","status":"In Progress","statusCategory":"In Progress","issueType":"Story","assignee":"Alex Chen"}],
  "childrenTotal": 1
}
```

`comments.items` contains the latest `--comments N` entries. `comments.truncated` is `true` when `total > items.length`. Full thread via `jiracli show comments <KEY> --json`.

`portfolio`: `null` when absent (field is omitted in JSON); `IssueSummary` object (`key`, `summary`, `status`, `statusCategory`) when the issue belongs to a portfolio. Requires hierarchy configuration — see `jiracli config hierarchy`.

### Errors

- Missing key argument: `[stderr] issue key required`, exit 2.
- Not found: `[stderr] issue NOPE-1 not found (HTTP 404) — check the key, or your PAT may lack browse permission on the project`, exit 1.
- 401: `[stderr] PAT in keychain for profile "X" was rejected (HTTP 401) — run: jiracli auth reauth`, exit 1.

---

## `search <jql...>`

JQL search — all issues returned by default, including Done.

    jiracli search <jql...> [flags]

Multiple positional arguments are joined with a space. JQL should be quoted to avoid shell interpretation.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--limit N` | 50 | Results per page (max 100) |
| `--page N` | 1 | Page number, 1-indexed |
| `--exclude-done` | false | Exclude issues in the Done status category |
| `--category <cat>` | — | Filter by status category: `todo`, `in-progress`, `done`, `all` |
| `--assigned` | false | Restrict to issues assigned to the current user |
| `--fields <spec>` | — | Add/drop columns: bare name or `+name` to add, `-name` to drop |
| `--fields-only <list>` | — | Restrict to exactly these fields (replaces defaults; mutex with `--fields`) |
| `--json` | false | NDJSON output |
| `--profile <name>` | default | Credential profile |

### Default behaviour

All issues are returned by default, **including Done**. Use `--exclude-done` to hide them (equivalent to adding `statusCategory != "Done"` to the JQL).

The effective JQL is always echoed on the first line of plain-text output. `statusCategory` values are universal: `"To Do"`, `"In Progress"`, `"Done"`.

### Default columns

`key, status, type, priority, assignee, updated, summary`

`--fields` adds to the default columns:
- `--fields "description"` — adds a third line per result: a single-line, ~100-character wiki-markup-stripped preview.
- `--fields "timeestimate,resolution"` — adds a fourth line per result showing those fields with their labels (`Remaining: —  Resolution: —`). Any field beyond the default set that is not `description` appears here, always emitted (showing `—` when the field is null/unset on that issue).
- Combine freely: `--fields "description,resolution,reporter"`.

`--fields-only "key,summary,description"` restricts to exactly those fields (mutex with `--fields`). `type` renders from `issueType`; the NDJSON field is `issueType`.

**Known extra-field labels:** `resolution` → `Resolution`, `timeestimate` → `Remaining`, `timeoriginalestimate` → `Estimate`, `timespent` → `Spent`, `reporter` → `Reporter`, `fixVersions` → `Fix Version`, `duedate` → `Due`. Time values are formatted as `2h30m`. Unknown field IDs appear with the raw ID as the label.

### Plain-text output shape

```
search: (<effective JQL>)
total: 14  page: 1/1
→ jiracli show WEB-812  # (and any key below)

[1] Bug  WEB-812  Summary text                                    In Progress
    Prio: High  Assignee: Alex Chen  Updated: 2d ago

[2] Story  WEB-799  Summary text                                  Open
    Prio: Medium  Assignee: —  Updated: 5d ago

→ jiracli show WEB-799  # (and any key above)
--- page 1 of 1 ---
[exit:0 | Xms]
```

With `--fields "description"`, a third line per issue shows a description preview (4-space indent, ≤100 chars, ending in `…` if clipped). Wiki-markup macros (`{panel}`, `{color}`, `{code}`, etc.) and formatting markers are stripped.

With any other extra field (e.g. `--fields "timeestimate,resolution"`), a further line shows `Label: value` pairs — always present, showing `—` when the field has no value for that issue:

```
[1] Bug  WEB-812  Summary text                                    In Progress
    Prio: High  Assignee: Alex Chen  Updated: 2d ago
    Fix the login page for users with long email addresses so tha…
    Remaining: 4h  Resolution: —

[2] Story  WEB-799  Summary text                                  Open
    Prio: Medium  Assignee: —  Updated: 5d ago
    Remaining: —  Resolution: Fixed

--- page 1 of 1 ---
[exit:0 | Xms]
```

`↻ 2d` = last updated 2 days ago. When more pages exist:

    --- page 1 of 5 | next: jiracli search --page 2 --limit 50 "<jql>" ---

The next-page command includes every active flag (`--exclude-done`, `--fields`, etc.) verbatim.

### NDJSON output (`--json`)

One object per issue, then an optional `_pagination` trailer when more pages exist:

```ndjson
{"key":"WEB-812","summary":"...","description":"Fix the login page…","status":"In Progress","statusCategory":"In Progress","assignee":{"name":"u1","displayName":"Alex Chen"},"priority":"High","issueType":"Bug","updated":"...","labels":[...],"components":[...]}
{"_pagination":{"page":1,"pages":5,"total":217,"next_page":2,"has_more":true}}
```

The `_pagination` object is emitted as the last line only when `has_more` is true. Consumers can ignore objects whose top-level key starts with `_`.

---

## `assigned`

Convenience wrapper over `search`. Defaults to `assignee = currentUser() AND statusCategory != "Done" ORDER BY updated DESC`.

    jiracli show assigned [--category <todo|in-progress|done|all>] [flags]

### Flags

| Flag | Description |
|---|---|
| `--category todo` | `statusCategory = "To Do"` |
| `--category in-progress` | `statusCategory = "In Progress"` |
| `--category done` | `statusCategory = "Done"` |
| `--category all` | No status filter |
| `--limit N` | Results per page (default 50, max 100) |
| `--page N` | 1-indexed page |
| `--json` | NDJSON output |
| `--profile <name>` | Credential profile |

Output is identical to `search`. The header line shows the effective JQL so the caller can see exactly what was run. The default-open filter is not applied separately — the JQL is pre-built and complete.

---

## `comments <KEY>`

Full comment thread, paginated.

    jiracli show comments <KEY> [flags]

Uses `GET /rest/api/2/issue/<KEY>/comment?startAt=N&maxResults=M&orderBy=created` — independent of the issue fetch.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--limit N` | 50 | Comments per page (max 200) |
| `--page N` | 1 | 1-indexed page |
| `--since <date>` | — | RFC3339, YYYY-MM-DD, or relative (`7d`, `24h`); post-filtered client-side |
| `--json` | false | NDJSON output |
| `--profile <name>` | default | Credential profile |

### Plain-text output shape

```
[1] <comment-id>  2026-06-22 14:30  Alex Chen (u1)
    > Body line 1
    > Body line 2

[2] <comment-id>  2026-06-21 09:00  Sam Patel (u2)
    > Body

--- page 1 of 3 | next: jiracli show comments ACME-123 --page 2 --limit 50 ---
```

### NDJSON output (`--json`)

One object per comment:

```ndjson
{"id":"9421","author":{"name":"u1","displayName":"Alex Chen"},"created":"...","updated":"...","body":"..."}
```

---

## `history <KEY>`

Paginated changelog for an issue.

    jiracli show history <KEY> [flags]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--limit N` | 50 | Entries per page |
| `--page N` | 1 | 1-indexed page |
| `--include-rank` | false | Include `Rank_` field changes (Jira drag-reorder noise; hidden by default) |
| `--since <date>` | — | Filter to entries on or after this date (RFC3339, YYYY-MM-DD, or relative like `7d`, `24h`) |
| `--json` | false | NDJSON output |
| `--profile <name>` | default | Credential profile |

### Endpoint fallback

1. `GET /rest/api/2/issue/<KEY>/changelog?startAt=N&maxResults=M` (Jira DC 8.7+).
2. On 404: falls back to `GET /rest/api/2/issue/<KEY>?expand=changelog`, slices embedded `changelog.histories[]`. This path is capped at the server's `maxResults` (typically 100); a warning footer is shown when the result appears capped.

### Plain-text output shape

```
2026-06-22 08:14  Alex Chen   status: In Progress → In Review
2026-05-22 11:00  Robin Lee   status: In Review → In Progress  ↩
2026-05-20 10:15  Alex Chen   description: updated, fixVersion: 2026.05 → 2026.06
2026-05-11 10:30  Sam Patel   Comment: Here is a short note → Here is a longer…
```

Field rendering rules (same as `show` activity):
- `description` changes always render as `description: updated` / `set` / `cleared` (body never shown).
- `Comment` changes where both sides are empty (the common case — Jira changelog omits comment bodies) render as `Comment: added`. When bodies are present, they are truncated to 120 chars per side.
- `environment`, `summary` changes are truncated to 120 chars per side.
- All other fields are shown in full.
- `↩` marks a status regression (move to a lower `statusCategory` than the previous state).
- `Rank_` entries are suppressed unless `--include-rank`.
- `--since` filters entries client-side after the page is fetched; combine with `--page`/`--limit` for large histories.

### NDJSON output (`--json`)

One object per changelog entry:

```ndjson
{"id":"19811","author":{"name":"u1","displayName":"Alex Chen"},"created":"...","changes":[{"field":"status","from":"Open","to":"In Progress","fromCategory":"To Do","toCategory":"In Progress"}]}
```

`fromCategory`/`toCategory` are populated from the cached statuses list (populated by `lookup statuses`). They are empty strings when the cache has not been populated.

---

## `transitions <KEY>`

Lists available transitions from the issue's current status.

    jiracli show transitions <KEY> [flags]

### Flags

| Flag | Description |
|---|---|
| `--json` | NDJSON output |
| `--profile <name>` | Credential profile |

### Plain-text output shape

```
ACME-123  current: In Progress

  11   To Do
  21   In Review
  31   Done                  → jiracli edit status ACME-123 31
  51   Block                 → jiracli edit status ACME-123 "Block"

[exit:0 | Xms]
```

Drill-down hints show `→ jiracli edit status <KEY> <id>` for each transition. Transition names with no spaces may also be used as the argument.

### NDJSON output (`--json`)

One object per transition:

```ndjson
{"id":"31","name":"Done","toStatus":"Done","toStatusCategory":"Done"}
```

---

## `hierarchy <KEY>`

Walks the full ancestor chain (Initiative → Epic → Subject) and fetches children appropriate to the subject's type.

    jiracli show hierarchy <KEY> [flags]

Requires hierarchy field IDs to be configured for the profile — run `jiracli setup --reconfigure` or `jiracli config hierarchy --rediscover` first.

### Flags

| Flag | Description |
|---|---|
| `--json` | NDJSON output (one object: the full chain) |
| `--profile <name>` | Credential profile |
| `--depth N` | Levels of descendants to fetch (default 1 = direct children; max 5) |
| `--status <filter>` | Limit displayed children by status: `open`, `closed`, `not-closed` |
| `--all` | Fetch all children (bypasses the 100-result default cap) |
| `--open` | Show only non-Done issues (shorthand for `--status open`) |
| `--flat` | Flat TSV output: one row per node (`depth`, `key`, `type`, `status`, `assignee`, `summary`). With `--json` emits NDJSON flat mode. |
| `--since <date>` | Only include issues updated on or after this date (`-2w`, `-1d`, `2024-01-01`). Bare durations (`2w`) have `-` prepended. |

### Walk behaviour

- **Ancestor walk**: follows Portfolio → Parent Link → typed `parent` field → Epic Link, up to 8 hops. Cycles are detected and stopped silently.
- **Children**:
  - Subject is an **Epic** → children via JQL `"Epic Link" = KEY` (one search call)
  - Subject is a **portfolio-level type** (Initiative, Programme, Feature, etc.) → children via JQL `"<portfolioFieldName>" = KEY`
  - Otherwise → subtasks from the subject's inline response (no extra call)
- Up to 100 children are returned; Done-last sort within the display cap of 15.

### `--depth` — recursive subtree

`--depth N` fetches N levels of descendants instead of just direct children. Default is 1 (today's behaviour). Maximum is 5.

```
jiracli show hierarchy ACME-50 --depth 2
```

With `--depth 2` on an Initiative, the output shows each Epic and, indented beneath it, the Epic's own children (Stories/Bugs/etc.):

```
▶ ACME-50         [Initiative]  Open            Modernise authentication platform
  ├─ ACME-100       [Epic]   In Progress    Jane Smith              Fix login redirect
  │  ├─ ACME-150     [Story]  To Do          Jane Smith              Reproduce on Safari
  │  └─ ACME-151     [Story]  Done           John Doe                Write regression test
  └─ ACME-200       [Epic]   Open           __Unassigned            Upgrade TLS stack
     └─ ACME-201     [Story]  Open           Alice Brown             Audit cipher suite list

[exit:0 | Xms]
```

When combined with `--status open`, hidden-count footers accumulate across all levels:
```
   (3 hidden by --open filter, 12 across all levels)
```

Each Epic whose children are all filtered out shows a `(no open children)` placeholder:
```
  ├─ ACME-100       [Epic]   In Progress    Jane Smith              Fix login redirect
  │  └─ (no open children)
```

### `--flat` — tabular output

`--flat` emits a tab-separated table instead of the tree. Header row is always present. Ancestors appear at negative depth; subject at depth 0; children at depth 1+.

```
depth	key	type	status	assignee	summary
0	ACME-50	Initiative	Open	Jane Smith	Modernise authentication platform
1	ACME-100	Epic	In Progress	John Doe	Fix login redirect
2	ACME-123	Story	To Do	Jane Smith	Reproduce on Safari

[exit:0 | Xms]
```

Combine with `--json` for NDJSON flat mode: one object per node.

```json
{"key":"ACME-50","depth":0,"issueType":"Initiative","status":"Open","assignee":"Jane Smith","summary":"Modernise authentication platform","isSubject":true}
{"key":"ACME-100","depth":1,"parentKey":"ACME-50","issueType":"Epic","status":"In Progress","assignee":"John Doe","summary":"Fix login redirect"}
{"key":"ACME-123","depth":2,"parentKey":"ACME-100","issueType":"Story","status":"To Do","assignee":"Jane Smith","summary":"Reproduce on Safari"}
```

### `--since` — activity filter

`--since <date>` restricts all fetched children (at every depth level) to issues updated on or after the given date. Combines well with `--depth 2` to show recent activity across an Initiative:

```
jiracli show hierarchy ACME-50 --depth 2 --since -2w --open
```

Accepted formats: Jira relative dates (`-2w`, `-1d`, `-30m`) and ISO dates (`2024-01-01`). Bare durations (`2w`) are accepted and have `-` prepended automatically.

### Plain-text output shape (depth 1)

```
ACME-50         [Initiative]  Open            Modernise authentication platform
ACME-100        [Epic]        In Progress     Fix login redirect
▶ ACME-123       [Bug]         In Progress     Fix login page timeout
  ├─ ACME-150    [Sub-task]    To Do           Jane Smith              Reproduce on Safari
  └─ ACME-151    [Sub-task]    Done            John Doe                Write regression test

[exit:0 | Xms]
```

Ancestor rows are dimmed (grey) when the terminal supports ANSI. The subject is prefixed with `▶`. Children use `├─` / `└─` tree connectors. When children are capped at 15, a `… N more` line is appended.

When the subject has no ancestors and no children:
```
▶ ACME-999       [Task]        Open            Standalone task
(standalone issue — no parent or children)
```

### NDJSON output (`--json`)

One object:

```json
{
  "ancestors": [
    {"key":"ACME-50","summary":"Modernise authentication platform","status":"Open","statusCategory":"To Do","issueType":"Initiative"},
    {"key":"ACME-100","summary":"Fix login redirect","status":"In Progress","statusCategory":"In Progress","issueType":"Epic"}
  ],
  "subject": {"key":"ACME-123","summary":"Fix login page timeout","status":"In Progress","statusCategory":"In Progress","issueType":"Bug","isSubject":true},
  "children": [
    {"key":"ACME-150","summary":"Reproduce on Safari","status":"To Do","statusCategory":"To Do","issueType":"Sub-task","assignee":"Jane Smith"},
    {"key":"ACME-151","summary":"Write regression test","status":"Done","statusCategory":"Done","issueType":"Sub-task","assignee":"John Doe"}
  ],
  "childrenTotal": 2
}
```

With `--depth 2`, each child node may carry a nested `"children"` array of its own (omitted when empty):

```json
{
  "ancestors": [],
  "subject": {"key":"ACME-50","summary":"Modernise authentication platform","status":"Open","statusCategory":"To Do","issueType":"Initiative","isSubject":true},
  "children": [
    {
      "key": "ACME-100",
      "summary": "Fix login redirect",
      "status": "In Progress",
      "statusCategory": "In Progress",
      "issueType": "Epic",
      "assignee": "Jane Smith",
      "children": [
        {"key":"ACME-150","summary":"Reproduce on Safari","status":"To Do","statusCategory":"To Do","issueType":"Story","assignee":"Jane Smith"},
        {"key":"ACME-151","summary":"Write regression test","status":"Done","statusCategory":"Done","issueType":"Story","assignee":"John Doe"}
      ]
    }
  ],
  "childrenTotal": 1
}
```

### Truncation

When `--depth >= 2` and any level-2+ batch hit the 100-result cap without `--all`, the output appends:
```
   (some subtrees may be incomplete — rerun with --all to fetch every descendant)
```
In JSON mode, `"descendantsTruncated": true` is set on the root object.

### Errors

- Hierarchy not configured: `[stderr] hierarchy not configured for profile "default" — run: jiracli setup --reconfigure`, exit 1.
- Non-issue ref: `[stderr] show hierarchy requires a plain issue key — got "ACME-123:comment:9421"`, exit 1.

---

## `attachments <KEY>`

Lists attachments on an issue (no download).

    jiracli show attachments <KEY> [flags]

### Flags

| Flag | Description |
|---|---|
| `--json` | NDJSON output |
| `--profile <name>` | Credential profile |

### Plain-text output shape

```
1   trace.har        142 KB   2026-05-11  Alex Chen
    → jiracli show attachment ACME-123:attach:11001

2   logcat.txt         9 KB   2026-05-11  Alex Chen
    → jiracli show attachment ACME-123:attach:11002
```

NDJSON emits one object per attachment matching the `attachments[]` shape inside the issue record: `{"id":"11001","filename":"trace.har","mimeType":"application/json","size":145000,"uploaded":"...","author":"u1"}`.

---

## `attachment download <ref>`

Downloads a single attachment to disk.

    jiracli show attachment <KEY>:attach:<id> [-o <path>]

`<ref>` must be the `<KEY>:attach:<id>` form. Plain issue keys and `:comment:` refs are rejected with a corrective hint.

### Flags

| Flag | Default | Description |
|---|---|---|
| `-o <path>` | `/tmp/jiracli-attach/<id>-<filename>` | Output path |
| `--profile <name>` | default | Credential profile |

No `--json` flag. Output is always plain text.

### Output

    ✓ saved /tmp/jiracli-attach/11001-trace.har

The default path is deterministic and agent-grepable: `/tmp/jiracli-attach/<id>-<filename>`.

### Errors

- Invalid ref form: `[stderr] <input> is not a valid attachment reference — expected <KEY>:attach:<id>`
- Not found (HTTP 404): corrective message naming the issue key and suggesting `jiracli show attachments <KEY>`.

---

## `open <ref>`

Opens an issue, comment, or attachment in the default browser. Accepts any ref form from the grammar table above.

    jiracli open <ref> [--print-url] [--profile <name>]

### Flags

| Flag | Description |
|---|---|
| `--print-url` | Print the URL to stdout instead of opening the browser |
| `--profile <name>` | Credential profile |

No `--json` flag.

### URL construction

| Ref form | Resulting URL |
|---|---|
| `ACME-123` | `<baseURL>/browse/ACME-123` |
| `ACME-123:comment:9421` | `<baseURL>/browse/ACME-123?focusedCommentId=9421&page=com.atlassian.jira.plugin.system.issuetabpanels:comment-tabpanel` |
| `ACME-123:attach:11001` | `<baseURL>/secure/attachment/11001/<url-encoded-filename>` (fetches attachment metadata first to resolve the filename) |

Browser: `open` on macOS, `xdg-open` on Linux. On other OS: prints the URL to stdout with `[stderr] cannot auto-open on this OS — copy the URL above`.

### Pagination and overflow

All paginated commands (`search`, `comments`, `history`, `assigned`) emit a footer:

- More pages exist: `--- page M of N | next: jiracli <cmd> --page <M+1> [flags] "<args>" ---`
- Last page: `--- page M of N ---`

Outputs exceeding ~200 lines or ~50 KB are truncated; the full content is written to `/tmp/jiracli-output/output-N.txt`. The truncated output includes hints to use `grep`, `head`, or `tail` on that file.
