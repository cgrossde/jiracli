# jiracli — Boards and Sprints

Reference for: `board list`, `board show`, `board issues`, `sprint list`, `sprint show`, `sprint issues`, `sprint current`, `edit sprint`, `config agile`.

All commands accept `--profile <name>` and `--json`. Paginated commands accept `--limit` and `--page`.

These commands call the Agile REST API (`/rest/agile/1.0/...`). Sprint commands only work with **scrum** boards. Kanban boards reject all sprint endpoints — see [Kanban restriction](#kanban-restriction).

Sprint enrichment on `show <KEY>` and `search --fields sprint` requires `config agile` to have discovered the Sprint custom field. Run `jiracli setup` or `jiracli config agile --rediscover` to configure it.

---

## `board list` / `lookup boards`

Lists Agile boards visible to the current user, filtered to a project.

    jiracli board list --project <KEY> [flags]
    jiracli lookup boards --project <KEY> [flags]

Both forms are identical. `board list` is the canonical name; `lookup boards` is the alias that keeps the surface consistent with `lookup projects`.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--project <KEY>` | — | **Required.** Project key filter |
| `--type <type>` | — | Filter by board type: `scrum` or `kanban` (client-side) |
| `--limit N` | 50 | Results per page (max 100) |
| `--page N` | 1 | Page number, 1-indexed |
| `--no-cache` | false | Bypass local cache |
| `--json` | false | NDJSON output |
| `--profile <name>` | default | Credential profile |

`--project` is required. Without it the Agile board endpoint returns the full instance list, which is too noisy. If omitted:

```
[stderr] --project required — run: jiracli lookup projects
[exit:1 | Xms]
```

### API call

`GET /rest/agile/1.0/board?projectKeyOrId=<KEY>&startAt=<offset>&maxResults=<limit>`

Page 1 with `limit ≥ 50` is cached for **1 hour** per project key. Pages 2+ and smaller limits skip cache.

### Plain-text output shape

```
Boards for ACME  (page 1)

 101  Car Release Board  scrum
 102  Car Kanban Flow    kanban

→ jiracli sprint current --board 101
→ jiracli board issues 102
→ jiracli sprint list --board 101
[exit:0 | Xms]
```

Columns: `id`, `name`, `type`. Scrum boards emit the `sprint current` drill hint; kanban boards emit `board issues`.

When `has_more` is true, a pagination trailer is appended:

```
→ jiracli board list --project ACME --page 2 --limit 50
```

### NDJSON output (`--json`)

One object per board:

```json
{"id":101,"name":"Car Release Board","type":"scrum"}
{"id":102,"name":"Car Kanban Flow","type":"kanban"}
```

When more pages exist, a pagination trailer is appended as the final line:

```json
{"_pagination":{"page":1,"pages":-1,"total":-1,"next_page":2,"has_more":true}}
```

> **Note:** The Agile board list endpoint reports `total` record count but not page count. `pages` and `total` in the pagination trailer are set to `-1` as sentinels. Use `has_more` as the canonical signal for whether another page exists.

### Errors

- No `--project`: `[stderr] --project required — run: jiracli lookup projects`, exit 1.
- Not found / no boards: `[stderr] no boards found for project ACME`, exit 1.
- 401: `[stderr] PAT in keychain for profile "X" was rejected (HTTP 401) — run: jiracli auth reauth`, exit 1.

---

## `board show`

Fetches the configuration for a single board: columns, statuses, filter, and sub-query.

    jiracli board show <id-or-name> [--project <KEY>] [flags]

For name resolution, `--project` is required. Numeric IDs are used directly without lookup.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--project <KEY>` | — | Required for name resolution; not needed for numeric IDs |
| `--details` | false | Fetch filter details: owner name, active status, and JQL (one extra API call to `GET /rest/api/2/filter/<id>`) |
| `--no-cache` | false | Bypass local cache |
| `--json` | false | NDJSON output |
| `--profile <name>` | default | Credential profile |

### API calls

1. `GET /rest/agile/1.0/board?projectKeyOrId=<KEY>` — only when resolving a name; result cached 1h.
2. `GET /rest/agile/1.0/board/<id>/configuration` — board config; cached **1 hour**.

### Plain-text output shape

```
Board 101  Car Release Board  scrum
Filter:   10200
SubQuery: (none)

Columns:
  Backlog        statuses: [10001, 10002]
  Open
  To Do          statuses: [10003]
  In Progress    statuses: [10004]
  Review         statuses: [10005]
  Done           statuses: [10006, 10007]

Drill in:
  → jiracli board issues 101
  → jiracli sprint list --board 101
[exit:0 | Xms]
```

`SubQuery: (none)` is shown when the board has no sub-filter. The sprint drill hint is omitted for kanban boards.

With `--details`, the filter owner and JQL are shown:

```
Board 101  PROJ Release Scrum  scrum
Filter:   10200  "PROJ Release Filter"
Owner:   Jane Smith (active)
JQL:     project = PROJ AND issuetype in standardIssueTypes()

Columns:
  ...
```

### NDJSON output (`--json`)

Single object:

```json
{
  "id": 101,
  "name": "Car Release Board",
  "type": "scrum",
  "filterId": "10200",
  "columns": [
    {"name": "Backlog", "statusIds": ["10001", "10002"]},
    {"name": "Open", "statusIds": []},
    {"name": "To Do", "statusIds": ["10003"]},
    {"name": "In Progress", "statusIds": ["10004"]},
    {"name": "Review", "statusIds": ["10005"]},
    {"name": "Done", "statusIds": ["10006", "10007"]}
  ]
}
```

> `filterId`: string; omitted (`omitempty`) when the board has no backing filter. With `--details`, the JSON also includes a `filter` object: `{"id":"10200","name":"PROJ Release Filter","jql":"project = PROJ AND ...","ownerName":"alice","ownerActive":true,"editable":false}`.

### Errors

- Ambiguous name (multiple boards match): error listing each candidate with `--board <id>` hints, exit 1.
- `--project` missing when resolving by name: `[stderr] --project required for name resolution — use a numeric board id or add --project <KEY>`, exit 1.
- Board not found: `[stderr] board 999 not found`, exit 1.

---

## `board issues`

Lists issues on a board using the Agile board endpoint.

    jiracli board issues <id> [flags]

`<id>` is a numeric board ID. Name resolution is not supported here (it would require a `--project` flag the user may not have supplied).

### Flags

| Flag | Default | Description |
|---|---|---|
| `--limit N` | 50 | Results per page (max 100) |
| `--page N` | 1 | Page number, 1-indexed |
| `--fields <spec>` | — | Add/drop columns (same syntax as `search --fields`) |
| `--fields-only <list>` | — | Restrict to exactly these fields |
| `--keys-only` | false | Print one key per line |
| `--json` | false | NDJSON output |
| `--profile <name>` | default | Credential profile |

### API call

`GET /rest/agile/1.0/board/<id>/issue?startAt=<offset>&maxResults=<limit>&fields=<csv>`

Board issue results are **not cached**.

### Plain-text output shape

```
board: 102  Car Kanban Flow  (page 1 of 3)

[1] Story  ACME-123  Login page crashes on iOS 17                In Progress
    Prio: High  Assignee: alice  Updated: 2d ago

[2] Bug    ACME-124  Export to CSV silently drops rows            To Do
    Prio: Medium  Assignee: bob  Updated: 5h ago

→ jiracli board issues 102 --page 2 --limit 50
[exit:0 | Xms]
```

The header line is `board: <id>  <name>  (page N of M)`. The board name comes from one `GET /board/<id>/configuration` call (cached 1h). Output columns and `--fields` behaviour are identical to `search`.

### NDJSON output (`--json`)

Issues emit in the same schema as `search --json`. Pagination trailer when more pages exist:

```json
{"key":"ACME-123","summary":"Login page crashes on iOS 17","status":"In Progress",...}
{"key":"ACME-124","summary":"Export to CSV silently drops rows","status":"To Do",...}
{"_pagination":{"page":1,"pages":3,"total":127,"next_page":2,"has_more":true}}
```

### Errors

- Invalid (non-numeric) ID: `[stderr] board id must be numeric`, exit 2.
- Board not found: `[stderr] board 999 not found`, exit 1.

---

## Kanban restriction

All `sprint` subcommands and the `edit sprint current/next` targets reject kanban boards at runtime. When a board is kanban, the Agile API returns HTTP 400 with `"The board doesn't support sprints."` The CLI maps this to:

```
[stderr] board 102 is kanban and does not support sprints — use: jiracli board issues 102
[exit:1 | Xms]
```

This message is emitted by: `sprint list`, `sprint current`, `sprint show` (when the origin board is kanban), `edit sprint current`, and `edit sprint next`.

---

## `sprint list`

Lists sprints on a scrum board.

    jiracli sprint list --board <id> [flags]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--board <id>` | — | **Required.** Scrum board ID |
| `--state <csv>` | `active,future` | Sprint states to include: `active`, `future`, `closed`, or `all` |
| `--limit N` | 50 | Results per page (max 100) |
| `--page N` | 1 | Page number, 1-indexed |
| `--name-contains <text>` | — | Case-insensitive substring filter on sprint name (client-side; fetches all sprints for the board) |
| `--after YYYY-MM-DD` | — | Keep sprints whose `endDate` is on/after this date |
| `--before YYYY-MM-DD` | — | Keep sprints whose `startDate` is on/before this date |
| `--sort asc\|desc` | `desc` for `--state closed`, else `asc` | Sort order by start date. Falls back to sprint ID when start date is absent. |
| `--no-cache` | false | Bypass local cache |
| `--json` | false | NDJSON output |
| `--profile <name>` | default | Credential profile |

`--state all` omits the state filter entirely. Any other value is a comma-separated subset of `active`, `future`, `closed`.

**Filter flags** fetch the full sprint list and apply matching client-side. For `--state closed`, page 1 is cached for 1 h, so repeated calls are cheap. `--sort` defaults to `desc` when `--state` is exactly `closed` — newest sprint first — matching the common "what ran in Q2?" question. Pass `--sort asc` to restore the legacy oldest-first order for scripts.

Cache TTL:
- `state == active,future` (default), page 1 → **1 hour** per board.
- `state == closed`, page 1 → **7 days** per board (cached as `closed-all` when filter flags are used).
- All other combinations, pages 2+: no cache.

### Plain-text output shape

```
Sprints for board 101  Car Release Board  (active, future)

 2001  active   Sprint 42   2026-05-01 → 2026-05-14
   → jiracli sprint issues 2001
 2002  future   Sprint 43   2026-05-15 → 2026-05-28
   → jiracli sprint issues 2002

[exit:0 | Xms]
```

Columns: `id`, `state`, `name`, `start → end`. When `is_last` is false, a pagination trailer is added:

```
--- next: jiracli sprint list --board 101 --page 2 --limit 50 ---
```

#### Filtered example — Q2 2026 sprints for a team

```sh
jiracli sprint list --board 101 --state closed --name-contains "Sprint" --after 2026-04-01
```

Returns only closed sprints whose name contains "Sprint" and whose end date is ≥ 2026-04-01, newest-first. Results are paginated using the `--page` flag; the first page is cached.

### NDJSON output (`--json`)

```json
{"id":2001,"name":"Sprint 42","state":"active","startDate":"2026-05-01","endDate":"2026-05-14","originBoardId":101}
{"id":2002,"name":"Sprint 43","state":"future","startDate":"2026-05-15","endDate":"2026-05-28","originBoardId":101}
```

Pagination trailer when `is_last` is false. The shape depends on the query:

```json
// Default (cached) query — total unknown, so pages/total are omitted:
{"_pagination":{"page":1,"next_page":2,"has_more":true}}

// Filtered/sorted query (--name-contains, --after, --before, --sort) — paged
// client-side over the full set, so real figures are reported:
{"_pagination":{"page":1,"pages":3,"total":120,"next_page":2,"has_more":true}}
```

`has_more` is always present and is the canonical "keep paging?" signal.

### Errors

- `--board` missing: `[stderr] --board required`, exit 2.
- Kanban board: [see Kanban restriction](#kanban-restriction).
- Board not found: `[stderr] board 999 not found`, exit 1.

---

## `sprint show`

Fetches details for a single sprint by numeric ID.

    jiracli sprint show <id> [flags]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--json` | false | NDJSON output |
| `--profile <name>` | default | Credential profile |

`<id>` must be numeric.

### API call

`GET /rest/agile/1.0/sprint/<id>`

### Plain-text output shape

```
Sprint 2001  Sprint 42  active
Dates:  2026-05-01 → 2026-05-14   (activated 2026-05-01)
Board:  101
Goal:   Stabilise login flow and close all P1s.

Drill in:
  → jiracli sprint issues 2001
[exit:0 | Xms]
```

`Goal: (none)` is shown when the sprint goal is empty. `activatedDate` is shown only when set.

### NDJSON output (`--json`)

```json
{
  "id": 2001,
  "name": "Sprint 42",
  "state": "active",
  "startDate": "2026-05-01",
  "endDate": "2026-05-14",
  "activatedDate": "2026-05-01",
  "originBoardId": 101,
  "goal": "Stabilise login flow and close all P1s."
}
```

### Errors

- Non-numeric ID: `[stderr] sprint id must be numeric`, exit 2.
- Sprint not found: `[stderr] sprint 9999 not found`, exit 1.

---

## `sprint issues`

Lists issues in a sprint.

    jiracli sprint issues <id> [flags]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--limit N` | 50 | Results per page (max 100) |
| `--page N` | 1 | Page number, 1-indexed |
| `--fields <spec>` | — | Add/drop columns (same syntax as `search --fields`) |
| `--fields-only <list>` | — | Restrict to exactly these fields |
| `--keys-only` | false | Print one key per line |
| `--json` | false | NDJSON output |
| `--profile <name>` | default | Credential profile |

### API call

`GET /rest/agile/1.0/sprint/<id>/issue?startAt=<offset>&maxResults=<limit>&fields=<csv>`

Sprint issue results are **not cached**.

### Plain-text output shape

```
sprint: 2001  Sprint 42  active  (page 1 of 2)

[1] Bug    ACME-123  Login page crashes on iOS 17                In Progress
    Prio: High  Assignee: alice  Updated: 2d ago
    → jiracli show ACME-123

[2] Story  ACME-124  Export to CSV silently drops rows            To Do
    Prio: Medium  Assignee: bob  Updated: 5h ago
    → jiracli show ACME-124

→ jiracli sprint issues 2001 --page 2 --limit 50
[exit:0 | Xms]
```

Each issue has a `→ jiracli show <KEY>` drill hint. Output columns and `--fields` behaviour match `search`.

### NDJSON output (`--json`)

Same schema as `search --json`. Pagination trailer appended on the last line when more pages exist:

```json
{"key":"ACME-123","summary":"Login page crashes on iOS 17","status":"In Progress",...}
{"key":"ACME-124","summary":"Export to CSV silently drops rows","status":"To Do",...}
{"_pagination":{"page":1,"pages":2,"total":75,"next_page":2,"has_more":true}}
```

### Errors

- Non-numeric ID: `[stderr] sprint id must be numeric`, exit 2.
- Sprint not found: `[stderr] sprint 9999 not found`, exit 1.

---

## `sprint current`

Fetches the active sprint on a board, then renders the sprint summary followed by its issues.

    jiracli sprint current --board <id> [flags]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--board <id>` | — | **Required.** Scrum board ID |
| `--assigned` | false | Show only issues assigned to the current user (client-side filter) |
| `--exclude-done` | false | Hide Done issues (client-side filter) |
| `--json` | false | Single composite JSON object: `{sprint, issues, returned, total, notes}` |
| `--profile <name>` | default | Credential profile |

### Behaviour

Calls `ListSprints(board, ["active"], page=1, limit=50)` and branches on the result count:

| Active sprints | Outcome |
|---|---|
| 0 | Error; suggests listing future sprints |
| 1 | Render sprint summary followed by up to 25 issues |
| > 1 | Error; lists each sprint with ID and name |

`--assigned` and `--exclude-done` are applied client-side after the issue list is fetched. They do not affect the API call, and they apply identically in plain-text and `--json` mode (the same filtered set is returned by both).

### Plain-text output shape — 1 active sprint

```
Sprint 2001  Sprint 42  active
Dates:  2026-05-01 → 2026-05-14   (activated 2026-05-01)
Board:  101
Goal:   Stabilise login flow and close all P1s.

Issues (page 1, showing 25 of 42):

[1] Bug    ACME-123  Login page crashes on iOS 17                In Progress
    Prio: High  Assignee: alice  Updated: 2d ago
    → jiracli show ACME-123

[2] Story  ACME-124  Export to CSV silently drops rows            To Do
    Prio: Medium  Assignee: bob  Updated: 5h ago
    → jiracli show ACME-124

→ jiracli sprint issues 2001 --limit 100
[exit:0 | Xms]
```

When all issues fit within 25 (no more pages), the `→ jiracli sprint issues` trailer is omitted.

### NDJSON output (`--json`)

A single composite object — "the current sprint and its issues" is one logical record, so there is no heterogeneous stream of sprint + issue lines and no pagination trailer. `issues` reflects the `--assigned`/`--exclude-done` filters; `total` is the sprint's full issue count (pre-filter), `returned` the count actually embedded:

```json
{"sprint":{"id":2001,"name":"Sprint 42","state":"active","originBoardId":101,"goal":"Stabilise login flow and close all P1s."},"issues":[{"key":"ACME-123","summary":"Login page crashes on iOS 17","status":"In Progress","...":"..."},{"key":"ACME-124","summary":"Export to CSV silently drops rows","status":"To Do","...":"..."}],"returned":2,"total":42,"notes":["multiple active sprints — using most recent: 2001 \"Sprint 42\". Others: 2002 (Sprint 43). Pass --sprint <id> to override."]}
```

`notes` is omitted when empty. See `docs/json-schema.md` for the full field table.

### Errors

**No active sprint:**

```
[stderr] no active sprint for board 101 — list options with: jiracli sprint list --board 101 --state future
[exit:1 | Xms]
```

**Multiple active sprints:**

```
[stderr] board 101 has 2 active sprints — run one of:
         jiracli sprint show 2001   OR   jiracli sprint issues 2001   (Sprint 42)
         jiracli sprint show 2002   OR   jiracli sprint issues 2002   (Sprint 43)
[exit:1 | Xms]
```

**Kanban board:** see [Kanban restriction](#kanban-restriction).

---

## `edit sprint`

Moves one or more issues into a sprint or to the backlog. Dry-run by default.

    jiracli edit sprint <KEY> [KEY...] <target> [flags]

The last positional argument is always the target. All preceding positional arguments are issue keys.

### Targets

| Target | Meaning | `--board` required? |
|---|---|---|
| `<numeric-id>` | Move into the sprint with that ID | No |
| `current` | Active sprint on the specified board | **Yes** |
| `next` | First future sprint on the specified board | **Yes** |
| `backlog` | Remove from any sprint (Agile backlog endpoint) | No |

Closed sprints are rejected. The `current` and `next` targets follow the same multi/zero-sprint error rules as `sprint current`.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--board <id>` | 0 | Board ID; required for `current` and `next` targets |
| `--yes` | false | Apply without confirmation |
| `--profile <name>` | default | Credential profile |

### Dry-run preview

Without `--yes`, the command prints a preview and exits 0. In an interactive TTY the CLI prompts `Apply? [y/N]:` reading from `/dev/tty`. Non-TTY: prints preview and exits 0 without applying.

Preview shape (sprint target):

```
DRY RUN — no changes made.

POST https://jira.example.com/rest/agile/1.0/sprint/2001/issue

Body:
  {
    "issues": ["ACME-123", "ACME-124"]
  }

Effect:
  move 2 issue(s) into sprint 2001 "Sprint 42" (active)

Validation:
  ✓ 2 keys parsed
  ✓ sprint 2001 is active
  ⚠ sprint started 7d ago — late-add

To apply:
  re-run with --yes
[exit:0 | Xms]
```

Preview shape (backlog target):

```
DRY RUN — no changes made.

POST https://jira.example.com/rest/agile/1.0/backlog/issue

Body:
  {
    "issues": ["ACME-123"]
  }

Effect:
  move 1 issue(s) to backlog

Validation:
  ✓ 1 key parsed

To apply:
  re-run with --yes
[exit:0 | Xms]
```

### API calls

| Target | Endpoint |
|---|---|
| `<id>`, `current`, `next` | `POST /rest/agile/1.0/sprint/<id>/issue` |
| `backlog` | `POST /rest/agile/1.0/backlog/issue` |

Body: `{"issues": ["ACME-123", "ACME-124"]}`.

After a successful move, cache entries `sprints/*` and `issue-summary/<KEY>` (for each moved key) are invalidated.

### Success output

```
✓ moved 2 issue(s) into sprint 2001 "Sprint 42"
  → jiracli show ACME-123
  → jiracli show ACME-124
[exit:0 | Xms]
```

When more than 5 keys were moved, the per-key drill hints are capped:

```
✓ moved 7 issue(s) into sprint 2001 "Sprint 42"
  → jiracli show ACME-123
  → jiracli show ACME-124
  → jiracli show ACME-125
  → jiracli show ACME-126
  → jiracli show ACME-127
  … and 2 more
```

Backlog success:

```
✓ moved 1 issue(s) to backlog
  → jiracli show ACME-123
```

### Partial failure

The Agile move endpoint accepts all keys in one POST. When Jira rejects individual keys, it returns a body of the form `{"errors":{"ACME-9":"...message..."}}`. These are surfaced as:

```
Failed:
  ACME-9: issue does not exist or you do not have permission to see it.
[exit:1 | Xms]
```

All successfully moved keys are still reported.

### Errors

- Invalid issue key: `[stderr] not a valid issue key: "foobar"`, exit 2.
- `--board` missing for `current`/`next`: `[stderr] --board required for target "current"`, exit 2.
- Closed sprint (numeric target): `[stderr] cannot move issues into closed sprint 2001`, exit 1.
- Sprint not found: `[stderr] sprint 9999 not found`, exit 1.
- Kanban board (for `current`/`next`): see [Kanban restriction](#kanban-restriction).

### Examples

```
jiracli edit sprint ACME-123 2001
jiracli edit sprint ACME-123 ACME-124 current --board 101 --yes
jiracli edit sprint ACME-123 next --board 101
jiracli edit sprint ACME-123 backlog --yes
```

---

## `config agile`

Views or updates the Agile / Sprint field configuration for a profile. Sprint enrichment on `show <KEY>` and `search --fields sprint` requires this field to be set.

    jiracli config agile [flags]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--rediscover` | false | Re-run Sprint field discovery; persists when changed |
| `--field <id-or-none>` | — | Explicitly set or clear the Sprint field ID; `none` clears it |
| `--json` | false | Output as a single JSON object |
| `--profile <name>` | default | Credential profile |

### Behaviour

**No flags:** prints the current configuration and runs a live probe (calls `GET /rest/api/2/field` with `--no-cache`) to show whether the configured field ID can still be resolved:

```
Sprint field:  customfield_10050
Discovered:    2026-05-10T09:00:00Z

Live probe:  ✓ customfield_10050 resolved as "Sprint" (com.pyxis.greenhopper.jira:gh-sprint)
[exit:0 | Xms]
```

When no field is configured:

```
Sprint field:  (not configured)

Live probe:  ✓ discovered customfield_10050 — run: jiracli config agile --rediscover to persist
[exit:0 | Xms]
```

**`--rediscover`:** calls `ResolveSprintField` against the live `/field` endpoint (bypasses cache), then persists the result. Resolution matches fields where `name == "Sprint"` AND `schema.custom == "com.pyxis.greenhopper.jira:gh-sprint"`. Substring matching is not used; if no exact match exists, nothing is persisted.

```
✓ Sprint field = customfield_10050
[exit:0 | Xms]
```

When no match is found:

```
  (Sprint field not found — Jira Software not installed?)
  Sprint commands will still work via Agile API.
[exit:0 | Xms]
```

**`--field <id>`:** resolves via `lookup fields` then persists. `--field none` clears the stored value.

```
✓ Sprint field set to customfield_10050
[exit:0 | Xms]
```

### NDJSON output (`--json`)

```json
{"sprintField":"customfield_10050","discoveredAt":"2026-05-10T09:00:00Z"}
```

When the field is not configured, `sprintField` is `""` and `discoveredAt` is omitted:

```json
{"sprintField":"","discoveredAt":null}
```

### Sprint field discovery algorithm

Used by `--rediscover` and by `jiracli setup` Step 4:

1. Fetch all fields from `GET /rest/api/2/field` (cached 1h by default; `--rediscover` bypasses cache).
2. Find fields where `name == "Sprint"` AND `schema.custom == "com.pyxis.greenhopper.jira:gh-sprint"`. Use the first match.
3. Fallback (if no schema-typed match): `name == "Sprint"` AND `schema.type == "array"`. Use the first match.
4. No match → return empty string; nothing is persisted.

Substring matching is **never** used. The algorithm picks nothing rather than guessing.

### Sprint enrichment in `show` and `search`

When `sprintField` is configured, `show <KEY>` appends a Sprint section after the Epic/Portfolio block:

```
Sprint: Sprint 42  active  2026-05-01 → 2026-05-14
  → jiracli sprint show 2001
```

Multiple sprints (e.g. when an issue was moved mid-sprint) are each on their own line. The section is omitted entirely when the issue has no sprint membership.

`search --fields sprint` adds a Sprint line to each search result row, showing the most recent active or future sprint (closed-only sprints are suppressed unless no other sprint exists). Enable by passing `--fields sprint`:

```
jiracli search --jql 'project = ACME' --fields sprint
```

### Errors

- `--field` and `--rediscover` are mutually exclusive: `[stderr] --field and --rediscover are mutually exclusive`, exit 2.
- Unknown field ID: `[stderr] field "customfield_99999" not found — run: jiracli lookup fields --custom`, exit 1.
- 401: `[stderr] PAT in keychain for profile "X" was rejected (HTTP 401) — run: jiracli auth reauth`, exit 1.

---

## Cache TTL reference

| Cache key pattern | TTL | Populated by |
|---|---|---|
| `boards/<KEY>` | 1 hour | `board list` / `lookup boards`, page 1, limit ≥ 50 |
| `board/<id>/config` | 1 hour | `board show`, `board issues` (name lookup) |
| `sprints/<boardID>/active+future` | 1 hour | `sprint list` with default state, page 1 |
| `sprints/<boardID>/closed` | 7 days | `sprint list --state closed`, page 1 |
| `sprints/<boardID>/names` | 1 hour | `sprint list --name-contains` or `--sort` (GreenHopper fast path) |
| `sprints/<boardID>/closed-all` | 7 days | `sprint list --state closed` with filter flags (full paged fetch) |

Sprint issue and board issue results are never cached. Cache entries for sprints are invalidated after a successful `edit sprint` move (`sprints/*` glob).

Use `jiracli cache list` to inspect live entries and `jiracli cache clear --key <key>` to purge a specific entry.
