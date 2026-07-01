# jiracli NDJSON v1 schemas

**v1 — additive changes only. Renaming or removing fields is a major version bump.**

All `--json` output is [NDJSON](https://ndjson.org/): one JSON object per line, no wrapping array.
Errors always go to stderr; stdout carries only records and optional trailer lines.
The `[exit:N | Xms]` footer is suppressed in `--json` mode.

### Trailer records

A *trailer* is a final line whose **sole top-level key is underscore-prefixed**, so consumers can skip it without inspecting the payload (`jq 'select(has("_pagination") or has("_meta") | not)'`). Two trailer kinds exist:

| Trailer | Meaning | Emitted by |
|---|---|---|
| `_pagination` | More pages are available; carries the next-page cursor/number. | Paginated list commands (search, comments, history, sprint list, …) |
| `_meta` | Aggregation summary for a command that consumes its whole result set (totals, source query, whether paging was cut short). | `search --count-by` |

Data records never start with `_`. A trailer is omitted entirely when it carries no signal (e.g. no more pages).

---

## issue

Command: `jiracli show <KEY> [--json]`

One record per invocation. Produced by `internal/jira.IssueRecord` (`internal/jira/issue.go`).

```json
{
  "key":             "ACME-123",
  "summary":         "Fix login redirect",
  "status":          "In Progress",
  "statusId":        "3",
  "statusCategory":  "In Progress",
  "resolution":      null,
  "priority":        "High",
  "issueType":       "Bug",
  "assignee":        { "name": "u1", "displayName": "Alex Chen" },
  "reporter":        { "name": "u2", "displayName": "Sam Lee" },
  "created":         "2026-01-10T08:00:00.000+0000",
  "updated":         "2026-06-20T14:30:00.000+0000",
  "description":     "Steps to reproduce...",
  "labels":          ["backend", "auth"],
  "components":      ["Login"],
  "fixVersions":     ["4.5.0"],
  "parent":          { "key": "ACME-100", "summary": "Auth epic", "status": "In Progress", "statusCategory": "In Progress" },
  "epic":            { "key": "ACME-100", "summary": "Auth epic", "status": "Open", "statusCategory": "To Do" },
  "portfolio":       { "key": "ACME-50", "summary": "Modernise authentication platform", "status": "Open", "statusCategory": "To Do" },
  "links": [
    {
      "id":           "10234",
      "type":         "Blocks",
      "direction":    "outward",
      "relationship": "blocks",
      "issue":        { "key": "ACME-124", "summary": "Related issue", "status": "Open", "statusCategory": "To Do" }
    }
  ],
  "attachments": [
    {
      "id":       "10042",
      "filename": "screenshot.png",
      "mimeType": "image/png",
      "size":     145678,
      "uploaded": "2026-06-01T10:00:00.000+0000",
      "author":   "Alex Chen"
    }
  ],
  "comments": {
    "total":     12,
    "truncated": true,
    "items": [
      {
        "id":      "9421",
        "author":  { "name": "u1", "displayName": "Alex Chen" },
        "created": "2026-06-20T09:00:00.000+0000",
        "updated": "2026-06-20T09:00:00.000+0000",
        "body":    "LGTM"
      }
    ]
  },
  "historyTruncated":  false,
  "historyTotal":      8,
  "activityTimeline": [
    {
      "type":    "transition",
      "author":  { "name": "u1", "displayName": "Alex Chen" },
      "created": "2026-06-19T15:00:00.000+0000",
      "changes": [
        { "field": "status", "from": "Open", "to": "In Progress", "fromCategory": "To Do", "toCategory": "In Progress" }
      ]
    }
  ],
  "children": [
    {"key":"ACME-124","summary":"Fix timeout in checkout","status":"In Progress","statusCategory":"In Progress","issueType":"Story","assignee":"Jane Smith"}
  ],
  "timetracking": {
    "originalEstimateSeconds":  144000,
    "remainingEstimateSeconds": 115200,
    "timeSpentSeconds":         28800
  },
  "storyPoints": 5,
  "sprints": [
    {"id": 2001, "name": "Sprint 42", "state": "active", "startDate": "2026-06-15", "endDate": "2026-06-29"}
  ],
  "childrenTotal": 1
}
```

**Field notes:**
- `resolution`: `null` when unresolved; string (e.g. `"Fixed"`) when resolved.
- `statusId`: string; the Jira status ID (e.g. `"3"` for In Progress). Omitted (`omitempty`) when the issue response does not include it. Stable — status IDs do not change when a status is renamed. Cached 7 days (`statuses` key).
- `assignee`, `reporter`: `null` when unset.
- `parent`, `epic`: `null` when absent.
- `portfolio`: `null` when absent (field omitted — `omitempty`); `IssueSummary` object with `key`, `summary`, `status`, `statusCategory`. Populated when hierarchy is configured for the profile and the issue has a portfolio-level parent. Summary is fetched with one extra API call.
- `comments.truncated`: `true` when `total > items.length` (the issue view inlines only the single latest comment; use `show comments <KEY>` for the full thread).
- `historyTruncated`: `true` only on the changelog fallback path (DC <8.7) when the server capped results.
- `timetracking`: object with `originalEstimateSeconds`, `remainingEstimateSeconds`, `timeSpentSeconds` (all `int64`). Omitted (`omitempty`) when absent or all-zero. Fetched on every default `show` call — no extra API round-trip.
- `storyPoints`: `float64` | absent. Story Points value from the instance-specific custom field. Omitted when the field is not configured (`jiracli setup` discovers it) or not set on the issue.
- `sprints`: array of `SprintRef`; omitted (`omitempty`) when the issue has no sprint data or the sprint field is not configured (`jiracli config agile`). Each entry: `id` (int), `name` (string), `state` (`"active"`, `"future"`, `"closed"`), `startDate`/`endDate` (ISO-8601 strings; omitted when absent).
- `activityTimeline[].type`: `"transition"` when the entry contains a `status` field change; `"update"` otherwise.
- `activityTimeline[].changes[].fromCategory`/`toCategory`: populated for `status` field changes; empty string otherwise.
- `children`: always present as an array (never `null`); empty `[]` when the issue has no children or `--no-children` is passed. Sub-tasks come from the issue response inline; Epic children are fetched via a separate search call.
- `childrenTotal`: total count of children. May exceed `len(children)` when an Epic has more than 100 children (only the first 100 are returned).
- `childrenError`: string; omitted (`omitempty`) when empty. Populated when the children search call fails (e.g. the project does not support `Epic Link`); the rest of the issue record is still returned.
- `links[].id`: numeric string; the link ID required by `jiracli delete link <id>`. Empty string if the Jira instance omits it (non-standard).
---

## search

Command: `jiracli search <jql...> [--json]`  
Command: `jiracli show assigned [--json]`

One record per issue. Produced by `internal/jira.SearchIssueRecord` (`internal/jira/search.go`).

```json
{
  "key":            "ACME-123",
  "summary":        "Fix login redirect",
  "description":    "Steps to reproduce...",
  "status":         "In Progress",
  "statusId":       "3",
  "statusCategory": "In Progress",
  "assignee":       { "name": "u1", "displayName": "Alex Chen" },
  "reporter":       { "name": "u2", "displayName": "Sam Lee" },
  "priority":       "High",
  "issueType":      "Bug",
  "updated":        "2026-06-20T14:30:00.000+0000",
  "labels":         ["backend"],
  "components":     ["Login"],
  "fixVersions":    ["4.5.0"],
  "timetracking":   { "originalEstimateSeconds": 144000, "remainingEstimateSeconds": 115200, "timeSpentSeconds": 28800 },
  "storyPoints":    5,
  "sprints": [
    {"id": 2001, "name": "Sprint 42", "state": "active"}
  ]
}
```

When more pages exist, a pagination trailer is appended as the final line:

```json
{ "_pagination": { "page": 1, "pages": 4, "total": 187, "next_page": 2, "has_more": true } }
```

The trailer is **omitted** when all results fit on the current page.

**Field notes:**
- `assignee`: `null` when unassigned.
- `statusId`: string; the Jira status ID. Omitted (`omitempty`) when absent. Same value as in the `issue` schema — use it to join against `jiracli lookup statuses --json` output.
- `updated`: raw Jira timestamp string (`"2006-01-02T15:04:05.000-0700"` format).
- `labels`, `components`: empty array `[]` when absent.
- `description`: string; omitted (`omitempty`) when empty. Populated when `--fields "description"` (or `--fields-only`) is passed.
- `timetracking`: object with `originalEstimateSeconds`, `remainingEstimateSeconds`, `timeSpentSeconds`; omitted when absent or all-zero. Fetched in every default search (no extra flag required).
- `storyPoints`: `float64` | absent. Omitted when the profile's Story Points field is not configured or the issue has no value.
- `sprints`: same `SprintRef` array as issue; omitted when the sprint field is not configured or `--fields sprint` was not requested.
- `reporter`: `IssueUserRef` (`name`, `displayName`); omitted (`omitempty`) when the issue has no reporter or when `reporter` is not in the fetched field set.
- `fixVersions`: string array; omitted (`omitempty`) when empty or not fetched.

---

## comments

Command: `jiracli show comments <KEY> [--json]`

One record per comment. Produced by `cmd.commentRecord` (`cmd/comments.go`).

```json
{
  "id":      "9421",
  "author":  { "name": "u1", "displayName": "Alex Chen" },
  "created": "2026-06-20T09:00:00.000+0000",
  "updated": "2026-06-20T09:05:00.000+0000",
  "body":    "LGTM"
}
```

Pagination trailer (same shape as search, when more pages exist):

```json
{ "_pagination": { "startAt": 50, "maxResults": 50, "total": 120, "nextPage": 2 } }
```

---

## history

Command: `jiracli show history <KEY> [--json]`

One record per changelog entry. Produced by `cmd.historyRecord` (`cmd/history.go`).

```json
{
  "id":      "12345",
  "author":  { "name": "u1", "displayName": "Alex Chen" },
  "created": "2026-06-19T15:00:00.000+0000",
  "changes": [
    {
      "field":        "status",
      "from":         "Open",
      "to":           "In Progress",
      "fromCategory": "To Do",
      "toCategory":   "In Progress"
    },
    {
      "field":        "assignee",
      "from":         "",
      "to":           "Alex Chen",
      "fromCategory": "",
      "toCategory":   ""
    }
  ]
}
```

**Field notes:**
- `Rank_*` entries are omitted by default; include with `--include-rank`.
- `fromCategory`/`toCategory`: populated for `status` field changes only; empty string otherwise.

---

## transitions

Command: `jiracli show transitions <KEY> [--json]`

One record per available transition. Produced by `cmd.transitionRecord` (`cmd/transitions.go`).

```json
{
  "id":               "21",
  "name":             "Start Progress",
  "toStatus":         "In Progress",
  "toStatusCategory": "In Progress"
}
```

No pagination trailer (transition lists are small).

---

## attachments

Command: `jiracli show attachments <KEY> [--json]`

One record per attachment. Produced by `internal/jira.AttachmentRecord` (`internal/jira/issue.go`).

```json
{
  "id":       "10042",
  "filename": "screenshot.png",
  "mimeType": "image/png",
  "size":     145678,
  "uploaded": "2026-06-01T10:00:00.000+0000",
  "author":   "Alex Chen"
}
```

**Field notes:**
- `size`: bytes as integer.
- `author`: `displayName` string (not a user object).
- No pagination trailer (attachment lists are fetched in full).

---

## auth status

Command: `jiracli auth status [--json]`

One record per invocation. Produced inline in `cmd/me.go`.

```json
{
  "profile":       "default",
  "url":           "https://jira.example.com",
  "name":          "alice",
  "displayName":   "Jane Smith",
  "emailAddress":  "alice@example.com",
  "savedAt":       "2026-06-24T10:00:00Z",
  "active":        true,
  "authenticated": true,
  "error":         null
}
```

**Field notes:**
- `name`: the Jira username (`name` field from `/myself`).
- `active`: `true` when the account is active on the instance.
- `authenticated`: `false` when the PAT is rejected (HTTP 401); command exits 1.
- `error`: `null` on success; error string on failure.

---

## hierarchy

Command: `jiracli hierarchy <KEY> [--json] [--depth N] [--flat] [--since <date>] [--exclude-done|--open|--state <cat>]`

One record per invocation. Produced by `internal/jira.HierarchyChain` (`internal/jira/hierarchy.go`).

```json
{
  "ancestors": [
    {
      "key":            "ACME-50",
      "summary":        "Modernise authentication platform",
      "status":         "Open",
      "statusCategory": "To Do",
      "issueType":      "Initiative"
    },
    {
      "key":            "ACME-100",
      "summary":        "Fix login redirect",
      "status":         "In Progress",
      "statusCategory": "In Progress",
      "issueType":      "Epic"
    }
  ],
  "subject": {
    "key":            "ACME-123",
    "summary":        "Fix login page timeout",
    "status":         "In Progress",
    "statusCategory": "In Progress",
    "issueType":      "Bug",
    "isSubject":      true
  },
  "children": [
    {
      "key":            "ACME-150",
      "summary":        "Reproduce on Safari",
      "status":         "To Do",
      "statusCategory": "To Do",
      "issueType":      "Sub-task",
      "assignee":       "Jane Smith"
    },
    {
      "key":            "ACME-151",
      "summary":        "Write regression test",
      "status":         "Done",
      "statusCategory": "Done",
      "issueType":      "Sub-task",
      "assignee":       "John Doe"
    }
  ],
  "childrenTotal": 2,
  "siblings": [
    {"key":"PROJ-501","summary":"Add OAuth flow","status":"To Do","statusCategory":"To Do","issueType":"Story","assignee":"Jane Smith"},
    {"key":"ACME-123","summary":"Fix login page timeout","status":"In Progress","statusCategory":"In Progress","issueType":"Bug","assignee":"John Doe","isSubject":true},
    {"key":"PROJ-502","summary":"Update session tokens","status":"Done","statusCategory":"Done","issueType":"Story","assignee":"Jane Smith"}
  ],
  "siblingsTotal": 3,
  "descendantsTruncated": false
}
```

**Field notes:**
- `ancestors`: array of nodes, root-first (Initiative at index 0, Epic at index N-1). Empty array `[]` when the subject has no ancestors.
- `subject`: the issue passed as the argument. Always has `"isSubject": true`.
- `children`: up to 100 nodes at depth=1. For Epics: issues where `Epic Link = KEY`. For portfolio-level types: issues where `"<portfolioFieldName>" = KEY`. Otherwise: subtasks inline from the subject's response (no extra API call). With `--depth N` (N ≥ 2), each child node may carry a nested `"children"` array of its own — the field is `omitempty` so depth-1 output is byte-for-byte identical to today's (no `children` keys on level-1 nodes).
- `childrenTotal`: total server-side count at level 1; may exceed `len(children)` for large Epics. Descendants at level 2+ are not counted here.
- `childrenError`: string; omitted (`omitempty`) when empty. When set, `children` is empty and the error describes why the search failed.
- Node `assignee`: display-name string; omitted (`omitempty`) when unassigned or for ancestor rows.
- Node `isSubject`: `true` only on the subject node; omitted on all other nodes.
- Node `children` (recursive): present only when `--depth N` is ≥ 2. `omitempty` — absent on nodes whose children were not fetched or are empty. A depth-1 invocation never emits this field.
- `descendantsTruncated`: `false` when all descendants were fully fetched; `true` when any level-2+ batch hit the 100-result cap without `--all`. Omitted (`omitempty`) when `false`.
- `siblings`: array of `HierarchyNode` (co-children of the nearest ancestor), including the subject node with `"isSubject": true`. Sorted: non-Done first, subject first within its done-group. Omitted (`omitempty`) when the subject has no parent (root issue). Capped at 100 by default; `--all` fetches all.
- `siblingsTotal`: total server-side sibling count; may exceed `len(siblings)`. Omitted (`omitempty`) when zero.
- `siblingsTruncated`: `true` when siblings were capped at 100 and more exist. Omitted (`omitempty`) when false.
- `--flat --json` mode: emits NDJSON flat — one object per node with fields `key`, `depth` (int, ancestors negative), `parentKey` (omitted for the subject), `issueType`, `status`, `assignee` (omitted when unset), `summary`, `isSubject` (omitted unless true). No `HierarchyChain` wrapper — every node is its own line. `descendantsTruncated` is not emitted in flat mode.

---

## effort

Commands (formerly `show rollup`):
- `jiracli effort <KEY> [--json] [--depth N] [--group-by status|statusCategory] [--exclude-done|--open|--state <cat>] [--since <date>]` — hierarchy mode
- `jiracli effort jql '<JQL>' [--group-by assignee|status|statusCategory] [--json]` — JQL mode
- `jiracli effort sprint <id> [--group-by assignee|status|statusCategory] [--json]` — sprint mode

One record per invocation. Produced by `internal/jira.RollupTree` (`internal/jira/rollup.go`).

```json
{
  "subjectKey":       "ACME-100",
  "subjectIssueType": "Epic",
  "subjectSummary":   "Fix login page timeout",
  "subject": {
    "label":                   "Epic ACME-100 (own)",
    "originalEstimateSeconds": 864000,
    "remainingEstimateSeconds": 208800,
    "timeSpentSeconds":         864000,
    "storyPoints":              0,
    "pointedCount":             0,
    "totalCount":               1,
    "issueTypeCounts":          null
  },
  "rows": [
    {
      "label":                   "Level 1 — 8 Storys",
      "originalEstimateSeconds": 345600,
      "remainingEstimateSeconds": 288000,
      "timeSpentSeconds":         57600,
      "storyPoints":              22,
      "pointedCount":             5,
      "totalCount":               8,
      "issueTypeCounts":          { "Story": 8 }
    }
  ],
  "nodes": null,
  "hasDeeperLevel":  true,
  "maxFetchedDepth": 1
}
```

**`RollupRow` fields (`subject` and each entry in `rows`):**

| Field | Type | Notes |
|---|---|---|
| `label` | string | Human-readable row label, e.g. `"Level 1 — 8 Storys"` |
| `originalEstimateSeconds` | int64 | Planned time in seconds |
| `remainingEstimateSeconds` | int64 | Remaining time in seconds |
| `timeSpentSeconds` | int64 | Time logged in seconds |
| `storyPoints` | float64 | Sum of Story Points for this level |
| `pointedCount` | int | Items in this level that have SP set |
| `totalCount` | int | Total item count in this level |
| `truncated` | bool | `true` when the fetch was capped and more items exist; omitted (`omitempty`) when false |
| `issueTypeCounts` | map[string]int | Count per issue type, e.g. `{"Story":5,"Bug":3}`; omitted when empty |

**`RollupNode` fields** — the `nodes` array is always `null` in `effort` output (the per-child list was removed; use `jiracli hierarchy <KEY>` for a per-child breakdown). The type is documented here for JSON stability:

| Field | Type | Notes |
|---|---|---|
| `key` | string | Issue key |
| `summary` | string | Issue summary |
| `status` | string | Status name |
| `statusCategory` | string | `"To Do"`, `"In Progress"`, or `"Done"` |
| `issueType` | string | Issue type name |
| `assignee` | string | Display name of the assignee; omitted (`omitempty`) when unassigned |
| `originalEstimateSeconds` | int64 | Planned time |
| `remainingEstimateSeconds` | int64 | Remaining time |
| `timeSpentSeconds` | int64 | Logged time |
| `storyPoints` | float64 | Story Points; omitted (`omitempty`) when not set |
| `childrenTotal` | int | Server-reported count of this node's children; 0 when none |
| `hasChildren` | bool | `true` when `childrenTotal > 0`; convenience field |

**`RollupTree` top-level fields:**

| Field | Type | Notes |
|---|---|---|
| `subjectKey` | string | The queried issue key |
| `subjectIssueType` | string | Issue type of the subject |
| `subjectSummary` | string | Summary of the subject |
| `subject` | `RollupRow` | The subject's own time tracking and SP |
| `rows` | `[]RollupRow` | Hierarchy mode: level aggregates (one row at `--depth 1`, two at `--depth 2`). `--group-by` mode: status-grouped rows, one per distinct value, sorted canonically. JQL/sprint mode: group rows or a single Total row. |
| `nodes` | `[]RollupNode` | Always `null` (per-child list removed; use `jiracli hierarchy`). Retained for JSON stability. |
| `hasDeeperLevel` | bool | `true` when any L1 child has its own children |
| `maxFetchedDepth` | int | Highest depth actually fetched (1 or 2) |
| `groupBy` | string | `"assignee"`, `"status"`, or `"statusCategory"` when `--group-by` was used; omitted (`omitempty`) otherwise |

> **JQL / sprint mode:** for `effort jql` / `effort sprint`, `subjectIssueType` is `""` and `subject` fields are all-zero. The `rows` array carries the group rows (`--group-by`) or a single `Total` row. `hasDeeperLevel` is always `false`.

> **Hierarchy `--group-by` mode:** one `RollupTree` JSON object is emitted per fetched level as NDJSON (not a single object). Each object's `rows` carries the status-grouped rows for that level; `groupBy` is set on every emitted object.

No pagination trailer (single-object response per level).


---

## board list

Command: `jiracli board list --project <KEY> [--json]`
Command: `jiracli lookup boards --project <KEY> [--json]`

One record per board. Produced by `internal/jira.Board` (`internal/jira/agile.go`).

```json
{"id": 101, "name": "PROJ Release Scrum", "type": "scrum"}
```

**Field notes:**
- `type`: `"scrum"` or `"kanban"`.

Pagination trailer (when more pages exist):
```json
{"_pagination":{"page":1,"pages":-1,"total":-1,"next_page":2,"has_more":true}}
```
`pages` and `total` are `-1` (sentinel) — the Agile board list endpoint does not report total page count. `has_more` is the canonical signal.

---

## board show

Command: `jiracli board show <id> [--json]`

One record per invocation. Produced by `internal/jira.BoardConfig` (`internal/jira/agile.go`).

```json
{
  "id": 101,
  "name": "PROJ Release Scrum",
  "type": "scrum",
  "columns": [
    {"name": "To Do",       "statusIds": ["1", "10269"]},
    {"name": "In Progress", "statusIds": ["10020", "3"]},
    {"name": "Done",        "statusIds": ["6"]}
  ]
}
```

**Field notes:**
- `columns[].statusIds`: status IDs (strings) belonging to that column. Empty array when the column has no mapped statuses.

---

## sprint list

Command: `jiracli sprint list --board <id> [--json]`

One record per sprint. Produced by `internal/jira.Sprint` (`internal/jira/agile.go`).

```json
{
  "id":            2001,
  "name":          "Sprint 42",
  "state":         "active",
  "startDate":     "2026-06-15T00:00:00.000Z",
  "endDate":       "2026-06-29T23:59:59.000Z",
  "activatedDate": "2026-06-15T00:00:00.000Z",
  "originBoardId": 101,
  "goal":          "Ship login redesign"
}
```

**Field notes:**
- `state`: `"active"`, `"future"`, or `"closed"`.
- `startDate`, `endDate`, `activatedDate`, `goal`: omitted (`omitempty`) when absent.
- `originBoardId`: the board this sprint was created on (may differ from the queried board when sprints are shared).

Pagination trailer:
- All queries (the default recency view, `--all`, and filtered/sorted queries) page over an in-memory working set and report real figures: `{"_pagination":{"page":1,"pages":3,"total":120,"next_page":2,"has_more":true}}`.
- `has_more` is always present and is the canonical "keep paging?" signal. No trailer is emitted on the last page.

---

## sprint show / sprint current

Commands: `jiracli sprint show <id> [--json]`, `jiracli sprint current --board <id> [--json]`

`sprint show`: single `Sprint` object (same shape as sprint list above).

`sprint current`: a **single composite object** — "the current sprint and its issues" is one logical record, so it is not a heterogeneous stream of sprint + issue lines.

```json
{
  "sprint":   { "id": 2001, "name": "Sprint 42", "state": "active", "originBoardId": 101 },
  "issues":   [ { "key": "ACME-123", "summary": "...", "status": "In Progress", "...": "...same shape as search SearchIssueRecord..." } ],
  "returned": 12,
  "total":    18,
  "notes":    ["multiple active sprints — using most recent: 2001 \"Sprint 42\". …"]
}
```

| Field | Type | Notes |
|---|---|---|
| `sprint` | `Sprint` | The resolved current/override sprint. |
| `issues` | `[]SearchIssueRecord` | Embedded issue list (default first 25), **after** the `--assigned` / `--exclude-done` client-side filters — identical to plain-text output. |
| `returned` | int | Number of records in `issues` (post-filter). |
| `total` | int | Total issues in the sprint as reported by the Agile API (pre-filter). |
| `notes` | `[]string` | Informational lines (stale-sprint skips, ambiguity, issues-unavailable). Omitted (`omitempty`) when empty. |

No pagination trailer (single-object response).

---

## sprintRef (embedded in issue and search)

The `sprints` field on `IssueRecord` and `SearchIssueRecord` is an array of `SprintRef`:

```json
{"id": 2001, "name": "Sprint 42", "state": "active", "startDate": "2026-06-15", "endDate": "2026-06-29"}
```

|Field|Type|Notes|
|---|---|---|
|`id`|int|Sprint numeric ID|
|`name`|string|Sprint name|
|`state`|string|`"active"`, `"future"`, or `"closed"`|
|`startDate`|string|ISO-8601; omitted when not set|
|`endDate`|string|ISO-8601; omitted when not set|

The array is omitted (`omitempty`) when the issue has no sprint data or the sprint custom field is not configured.
