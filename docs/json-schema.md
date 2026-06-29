# jiracli NDJSON v1 schemas

**v1 — additive changes only. Renaming or removing fields is a major version bump.**

All `--json` output is [NDJSON](https://ndjson.org/): one JSON object per line, no wrapping array.
Errors always go to stderr; stdout carries only records and optional pagination trailers.
The `[exit:N | Xms]` footer is suppressed in `--json` mode.

---

## issue

Command: `jiracli show <KEY> [--json]`

One record per invocation. Produced by `internal/jira.IssueRecord` (`internal/jira/issue.go`).

```json
{
  "key":             "ACME-123",
  "summary":         "Fix login redirect",
  "status":          "In Progress",
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
  "childrenTotal": 1
}
```

**Field notes:**
- `resolution`: `null` when unresolved; string (e.g. `"Fixed"`) when resolved.
- `assignee`, `reporter`: `null` when unset.
- `parent`, `epic`: `null` when absent.
- `portfolio`: `null` when absent (field omitted — `omitempty`); `IssueSummary` object with `key`, `summary`, `status`, `statusCategory`. Populated when hierarchy is configured for the profile and the issue has a portfolio-level parent. Summary is fetched with one extra API call.
- `comments.truncated`: `true` when `total > items.length` (controlled by `--comments N`).
- `historyTruncated`: `true` only on the changelog fallback path (DC <8.7) when the server capped results.
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
  "statusCategory": "In Progress",
  "assignee":       { "name": "u1", "displayName": "Alex Chen" },
  "reporter":       { "name": "u2", "displayName": "Sam Lee" },
  "priority":       "High",
  "issueType":      "Bug",
  "updated":        "2026-06-20T14:30:00.000+0000",
  "labels":         ["backend"],
  "components":     ["Login"],
  "fixVersions":    ["4.5.0"]
}
```

When more pages exist, a pagination trailer is appended as the final line:

```json
{ "_pagination": { "page": 1, "pages": 4, "total": 187, "next_page": 2, "has_more": true } }
```

The trailer is **omitted** when all results fit on the current page.

**Field notes:**
- `assignee`: `null` when unassigned.
- `updated`: raw Jira timestamp string (`"2006-01-02T15:04:05.000-0700"` format).
- `labels`, `components`: empty array `[]` when absent.
- `description`: string; omitted (`omitempty`) when empty. Populated when `--fields "description"` (or `--fields-only`) is passed.
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

Command: `jiracli show hierarchy <KEY> [--json] [--depth N] [--flat] [--since <date>]`

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
- `--flat --json` mode: emits NDJSON flat — one object per node with fields `key`, `depth` (int, ancestors negative), `parentKey` (omitted for the subject), `issueType`, `status`, `assignee` (omitted when unset), `summary`, `isSubject` (omitted unless true). No `HierarchyChain` wrapper — every node is its own line. `descendantsTruncated` is not emitted in flat mode.
