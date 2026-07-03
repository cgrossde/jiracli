# jiracli — Read Commands

Reference for: `issue`, `search`, `assigned`, `comments`, `history`, `transitions`, `attachments`, `attachment download`, `open`. For `hierarchy` and `effort`, see their dedicated pages: [hierarchy.md](hierarchy.md), [effort.md](effort.md).

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

Issue keys are case-insensitive, matching Jira's own REST/JQL behavior: `acme-123`
and `ACME-123` both resolve to the same issue; the lowercase form is normalized
to uppercase before the request is made.

---

## `issue <KEY>`

Fetches a single issue with inline latest comment and changelog.

    jiracli show <KEY> [flags]

`<KEY>` accepts a bare key or a full browse URL.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--no-comments` | false | Omit comments section entirely (default view inlines the single latest comment) |
| `--no-history` | false | Omit activity/changelog section |
| `--no-children` | false | Skip the children list (one fewer API call) |
| `--parent` | false | Show this issue's parent instead (Parent Link → Parent → Epic Link) |
| `--fields <spec>` | — | Override field set (see below) |
| `--json` | false | NDJSON output |
| `--profile <name>` | default | Credential profile |

### API call

`GET /rest/api/2/issue/<KEY>?fields=key,summary,status,assignee,reporter,description,labels,components,priority,issuetype,created,updated,comment,fixVersions,parent,issuelinks,attachment,resolution&expand=changelog`

Never uses `fields=*all` or `expand=renderedFields`.

When a hierarchy configuration exists for the profile (set via `setup` or `config hierarchy`), the Epic Link, Parent Link, Portfolio, Story Points, and Sprint custom-field IDs are appended to the field list automatically.

### `--fields` spec

- **Add to default:** `--fields "description,reporter"` — adds those fields to the default set. (`+description` is accepted as an alias.)
- **Drop from default:** `--fields "-assignee,-priority"`.
- **Mixed:** `--fields "timetracking,-priority"`.
- **Restrict to a fixed set:** `--fields-only "key,summary,description"` — fetches and renders exactly those fields. Mutually exclusive with `--fields`.

Field names match Jira's own field IDs. Standard names usable with `--fields`:

| Name | Label shown | Notes |
|---|---|---|
| `summary` | — | Always included |
| `status` | — | Always included |
| `issuetype` | — | Always included |
| `priority` | `Prio` | Drop with `-priority` |
| `assignee` | `Assignee` | Drop with `-assignee` |
| `reporter` | `Reporter` | Add with `reporter` |
| `created` | `Created` | Add with `created` |
| `updated` | `Updated` | Add with `updated` |
| `description` | `Description` | Add with `description` |
| `labels` | `Labels` | Always fetched |
| `components` | `Components` | Always fetched |
| `fixVersions` | `Fix Versions` | Add with `fixVersions` |
| `resolution` | `Resolution` | Add with `resolution` |
| `duedate` | `Due` | Add with `duedate` |
| `timeestimate` | `Remaining` | Add with `timeestimate`; formatted as `2h30m` |
| `timeoriginalestimate` | `Estimate` | Add with `timeoriginalestimate` |
| `timespent` | `Spent` | Add with `timespent` |
| `timetracking` | `Estimates:` block | **Fetched by default.** Shows `Planned / Remaining / Spent` + progress bar when non-zero. |
| Story Points field ID | `Story Points:` | Fetched and displayed when discovered at setup (run `jiracli config hierarchy --json` to see `storyPointsField`). |
| `sprint` | `Sprint:` block | Add with `sprint`. Shows all sprints on the issue. Requires sprint field to be configured (`jiracli config agile`). |
| `customfield_XXXXX` | raw ID | Any custom field ID from `jiracli lookup fields` |
| any other built-in field (e.g. `environment`) | field's own display name | Rendered generically as `Label: value`; see below |

Fields with no dedicated struct field or renderer — most custom fields, and
built-in fields like `environment` that aren't in the table above — are still
fully supported: they're rendered generically as a `Label: value` line (using
the field's display name from the Jira instance), the same way `jiracli
search` formats its extra columns. Numeric time fields are formatted as
durations, and object-shaped values (users, versions, options) fall back to
their `name`/`displayName`/`value`. In `--json` mode these appear in the
`extraFields` array as `{"id", "label", "value"}` triples. A field is only
ever silently omitted when the fetched value is empty/null.

Any `--fields`/`--fields-only` token that doesn't match a default field, a raw
`customfield_NNN` id, or a name/id known to the Jira instance is reported back
rather than silently dropped: a `⚠ unknown field(s) ignored: ...` line is
appended in plain-text mode, and an `unknownFields` array is added to the
`--json` record. This catches typos and garbage input (e.g. `--fields
"ünïcödé,🎉"`) without failing the whole request — the rest of the issue is
still fetched and rendered normally.

### Plain-text output shape

```
[Bug]  ACME-123  In Progress · Prio: High · Resolution: Fixed
"Summary text"

Assignee:     Alex Chen                    Reporter:     Sam Patel
Created:      2026-05-10 (2mo ago)         Updated:      2026-06-22 (1w ago)
Fix Version:  4.5.0                        Due:          2026-07-01
Components:   AuthService                  Labels:       backend, auth

Estimates: Planned 40h · Remaining 32h · Spent 8h  [████████░░░░░░░░░░░░░░░░] · 20% spent
Story Points: 5

Portfolio: ACME-50  "Modernise authentication platform"  (Open)
  → jiracli hierarchy ACME-123

Epic: ACME-100  "Auth reliability work"  (In Progress)

Sprint: Sprint 42  active  2026-06-15 → 2026-06-29
  → jiracli sprint show 2001

Description:
  Body wrapped at 80 cols, 2-space indent.

Links (2):
  blocks              ACME-145            In Progress     Summary text                                        (id: 10234)
  relates to          ACME-201            Done            Summary text                                        (id: 10235)
  → jiracli delete ACME-123:link:<id>
  → jiracli add link ACME-123 OTHER-123 --type "is related to"

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
  → jiracli hierarchy         ACME-123
  → jiracli effort            ACME-123   # roll up time & story points across its children

[exit:0 | Xms]
```

Notes on the header and metadata block:
- The header line shows `Prio:` inline; `Resolution:` is appended after it (with a `·` separator) only when the issue has a resolution — an open issue shows no `Resolution:` segment at all.
- `Assignee`/`Reporter` show the display name only — usernames/emails are never printed.
- `Fix Version` and `Due` share a row below `Created`/`Updated`; `Due` is only rendered when a due date was fetched and set (e.g. via `--fields duedate`). `Components` and `Labels` share the next row when `Components` is short enough to fit the value column, otherwise each gets its own full-width line.
- `Estimates:` and its progress bar are on the same line.
- Any requested field with no dedicated section — a built-in field like `environment`, or any `customfield_NNNNN` — renders generically as `Label: value` directly above `Resolution`/`Description` (see the `--fields` table below). This works because `show` reuses the same field-label/value-formatting logic as `jiracli search`.
- The `→ jiracli effort` line is shown only when it would find something to roll up: portfolio-level types (Initiative, Feature, Programme, Theme) always show it (they roll up via the parent-link field regardless of what `show` fetched); an Epic shows it only when it actually has children. It never appears for Stories, Bugs, or Sub-tasks.

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
- Use `jiracli hierarchy <KEY>` to walk the full Initiative → Epic → Subject chain.
- Up to **15 children** are shown: non-Done first, Done last (stable sort within each group).
- When truncated, the heading reads `Children (15 of N shown)` and a `→ jiracli search` hint shows all.
- `Children: (none)` is shown explicitly when no children exist and `--no-children` is not set.
- JSON output always includes `"children": []`, `"childrenTotal": 0`, and optionally `"childrenError"`.

Comment section behavior:
- `total = 0`: section omitted.
- `total > 1`: drill-down hint appended: `→ jiracli show comments ACME-123   # full thread (N comments)`.
- `total > 50`: hint becomes `→ 50+ comments — use jiracli show comments ACME-123 --page 1`.
- The issue view inlines only the single latest comment; use `jiracli show comments <KEY>` for the full, paginated thread.

Sprint section:
- Omitted when the issue has no sprint data or the sprint field is not configured (`jiracli config agile`).
- When present, shows one line per sprint: `Sprint: <name>  <state>  <start> → <end>`.
- Each sprint line is followed by `  → jiracli sprint show <id>`.

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
  "storyPoints": 5,
  "sprints": [
    {"id": 2001, "name": "Sprint 42", "state": "active", "startDate": "2026-06-15", "endDate": "2026-06-29"}
  ],
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
  "childrenTotal": 1,
  "unknownFields": ["ünïcödé"]
}
```

`comments.items` contains the single latest comment. `comments.truncated` is `true` when `total > items.length`. Full thread via `jiracli show comments <KEY> --json`.

`portfolio`: `null` when absent (field is omitted in JSON); `IssueSummary` object (`key`, `summary`, `status`, `statusCategory`) when the issue belongs to a portfolio. Requires hierarchy configuration — see `jiracli config hierarchy`.

`sprints`: array of `SprintRef` objects; omitted (`omitempty`) when the issue has no sprint data or the sprint field is not configured. Each entry has `id` (int), `name`, `state` (`"active"`, `"future"`, `"closed"`), `startDate`, `endDate` (strings, ISO 8601; omitted when absent).

`unknownFields`: array of strings; omitted (`omitempty`) when empty. Any `--fields`/`--fields-only` tokens that matched neither a default field, a raw `customfield_NNN` id, nor a name/id known to the Jira instance — a typo'd or garbage field name, in other words. The rest of the issue record is still returned; the corresponding sections are simply absent since the field was never fetched.

### Errors

- Missing key argument: `issue key required`, exit 2.
- Not found: `issue NOPE-1 not found (HTTP 404) — check the key, or your PAT may lack browse permission on the project`, exit 1.
- 401: `PAT in keychain for profile "X" was rejected (HTTP 401) — run: jiracli auth reauth`, exit 1.

### Multi-key and stdin mode

`show` accepts multiple issue keys as positional arguments, or `-` to read keys from stdin:

    jiracli show ACME-123 ACME-124 ACME-125
    jiracli search --keys-only --assigned | jiracli show -

- Each issue is preceded by a `━━━ KEY (N/M) ━━━` rule.
- Per-key errors (not found, invalid ref) are printed inline; the loop continues.
- Pass `-` as the sole argument to read one key per line from stdin. Blank lines and lines starting with `#` are ignored — so `--keys-only` output pipes in without filtering.
- Compound refs (`:attach:`, `:comment:`) require a single argument and cannot be mixed with multi-key.

---

## `search [<jql...>]`

JQL search — all issues returned by default, including Done.

    jiracli search [<jql...>] [flags]
    jiracli search --jql '<full JQL query>' [flags]

Positional arguments are joined with a space to form the JQL query. When the
query contains quoted string literals (e.g. `text ~ "KSP"`), shell quoting can
mangle the join. Use `--jql` to pass the entire query as one shell argument,
bypassing the join:

    jiracli search --jql 'text ~ "login" AND project = ACME ORDER BY updated DESC'

### Flags

| Flag | Default | Description |
|---|---|---|
| `--jql <query>` | — | Entire JQL query as one string — bypasses arg joining; mutually exclusive with positional args |
| `--limit N` | 50 | Results per page (max 100) |
| `--page N` | 1 | Page number, 1-indexed |
| `--exclude-done` | false | Exclude issues in the Done status category |
| `--open` | false | Alias for `--exclude-done` |
| `--state <cat>` | — | Filter by status category: `todo`, `in-progress`, `done`, `all` |
| `--assigned` | false | Restrict to issues assigned to the current user; **also excludes Done** unless `--state` is set (pass `--state all` to include Done) |
| `--fields <spec>` | — | Add/drop columns: bare name or `+name` to add, `-name` to drop |
| `--fields-only <list>` | — | Restrict to exactly these fields (replaces defaults; mutex with `--fields`) |
| `--json` | false | NDJSON output |
| `--profile <name>` | default | Credential profile |
| `--keys-only` | false | Print one issue key per line — no headers, no footer, no overflow. Bypasses Layer 2 presenter. |
| `--time` | false | Show time-tracking columns: `Estimate`, `Remaining`, `Spent`. Shorthand for `--fields +timeoriginalestimate,+timeestimate,+timespent`. Ignored when `--fields-only` is used. |
| `--count-by <field>` | — | Aggregate all matching issues by this field; replaces the issue list with a count/percent histogram. Supported: `status`, `statusCategory`, `priority`, `assignee`, `issueType`, `resolution`, `project`. Paginates internally; `--page` is ignored. Aborts if more than 500 issues match unless `--all` or an explicit `--limit` is given (see below). Mutually exclusive with `--keys-only`. |

### Default behaviour

Bare JQL and `--jql` queries return all issues by default, **including Done**. Use `--exclude-done` (alias `--open`) to hide them (equivalent to adding `statusCategory != "Done"` to the JQL).

`--assigned` is the exception: on its own it **excludes Done** (it implies `statusCategory != "Done"`). Combine it with `--state all` to include Done, or another `--state` value to filter differently.

The effective JQL is always echoed on the first line of plain-text output. `statusCategory` values are universal: `"To Do"`, `"In Progress"`, `"Done"`.

### Columns and `--fields` reference

Default fields fetched: `key, status, issuetype, priority, assignee, updated, summary, labels, components, timetracking`. The `timetracking` field is fetched by default but only appears in `--json` output (via `timetracking` key) or in the `show` command's Estimates block. Plain-text search output remains unchanged.

`--fields` adds to or drops from the default display columns. Syntax: `"name"` or `"+name"` to add, `"-name"` to drop.

| Name | Label shown | Notes |
|---|---|---|
| `description` | _(preview line)_ | Stripped wiki markup, ≤100 chars |
| `reporter` | `Reporter` | Add with `reporter` |
| `labels` | `Labels` | Fetched by default (present in `--json`); add `+labels` to show it as a plain-text column |
| `components` | `Components` | Fetched by default (present in `--json`); add `+components` to show it as a plain-text column |
| `fixVersions` | `Fix Version` | Add with `fixVersions` |
| `resolution` | `Resolution` | Add with `resolution` |
| `duedate` | `Due` | Add with `duedate` |
| `timeestimate` | `Remaining` | Formatted as `2h30m` |
| `timeoriginalestimate` | `Estimate` | Formatted as `2h30m` |
| `timespent` | `Spent` | Formatted as `2h30m` |
| `sprint` | `Sprint` | Alias — resolved to the configured sprint custom field id. Requires `jiracli config agile`; the token is dropped silently when not configured. Renders compactly as the sprint name(s), comma-separated, with the active sprint marked `(active)` — e.g. `Sprint: Sprint 41, Sprint 42 (active)`. |
| `customfield_XXXXX` | raw ID | Any field ID from `jiracli lookup fields` |

`--fields-only "key,summary,description"` restricts to exactly those fields (mutex with `--fields`). `type` renders from `issueType`; the NDJSON field is `issueType`.

Unknown field IDs are accepted and displayed with the raw ID as the label, showing `—` when absent on an issue.

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

The next-page command includes every active flag (`--exclude-done`, `--fields`, `--jql`, etc.) verbatim. When the JQL contains double-quotes, `~`, or parentheses, the next-page hint automatically uses `--jql` instead of a positional argument.

### NDJSON output (`--json`)

One object per issue, then an optional `_pagination` trailer when more pages exist:

```ndjson
{"key":"WEB-812","summary":"...","description":"Fix the login page…","status":"In Progress","statusCategory":"In Progress","assignee":{"name":"u1","displayName":"Alex Chen"},"priority":"High","issueType":"Bug","updated":"...","labels":[...],"components":[...]}
{"_pagination":{"page":1,"pages":5,"total":217,"next_page":2,"has_more":true}}
```

The `_pagination` object is emitted as the last line only when `has_more` is true. Consumers can ignore objects whose top-level key starts with `_`.

### `--keys-only` — pipe-friendly output

    jiracli search --keys-only --assigned
    jiracli search --keys-only "project = ACME AND status = 'In Review'"

Prints one issue key per line. No headers, no formatting, no `[exit:N]` footer. The `[exit:N]` footer is suppressed entirely (same as `--json`). When more pages exist, the final line is:

    # next: jiracli search --page 2 --limit 50 --assigned --keys-only

The `# next:` line can be detected with `grep -v '^#'` if a pure-keys stream is needed. Also available on `show assigned --keys-only`.

Only the `key` field is fetched from the Jira API — no wasted field deserialization.

### `--count-by` — aggregation histogram

    jiracli search --jql '<query>' --count-by <field>

Replaces the issue list with a three-column count/percent table. Paginates internally — `--page` is ignored. Mutually exclusive with `--keys-only`.

Supported fields: `status`, `statusCategory`, `priority`, `assignee`, `issueType`, `resolution`, `project`.

**Safety cap.** Broad, unscoped `--count-by` queries can match tens or hundreds of thousands of issues on a large instance, taking minutes to hours to paginate to exhaustion. To guard against this:

- By default, if more than **500** issues match, the command aborts with a corrective error instead of running:

  ```
  count-by aborted: 227348 issues match this query, which exceeds the default safety cap of 500 —
  counting that many could take a long time. Re-run with --all to count every matching issue, or add
  --limit N to cap the count at N issues (the result will be reported as partial)
  ```

- Pass `--all` to bypass the cap and count every matching issue, however many there are.
- Pass an explicit `--limit N` to cap the count at `N` issues instead of aborting. The command then runs and reports the count as partial:

  ```
  search: updated >= -3d  (count by project)
  total: 200 of 227348 matched issues counted

  Project                      Count   Percent
  ────────────────────────────────────────────
  XPR                            200    100.0%
  ────────────────────────────────────────────
  Total                          200    100.0%

  ⚠ capped at --limit 200; 227348 issues matched in total — re-run with --all to count every match
  ```

- If the query matches 500 or fewer issues, `--all`/`--limit` are unnecessary — the count-by table runs exactly as before.

**Plain-text output shape (uncapped):**

```
search: issueType = Epic AND fixVersion = "v2026-Q2"  (count by status)
total: 22 issues

Status                    Count   Percent
────────────────────────────────────────
Open                          3    13.6%
In Progress                   7    31.8%
Pending Review                2     9.1%
Closed                       10    45.5%
────────────────────────────────────────
Total                        22   100.0%
```

`status` and `statusCategory` use canonical ordering (blocked → open → in-progress → done). Other dimensions sort by count descending.

**JSON output (`--json`):** one NDJSON record per value, then a `_meta` trailer. `_meta.matched` is the server-reported total match count (may exceed `_meta.total` when capped); `_meta.note` is present only when `truncated` is `true`:

```ndjson
{"dimension":"status","value":"Open","count":3,"percent":13.6}
{"dimension":"status","value":"In Progress","count":7,"percent":31.8}
{"_meta":{"total":22,"matched":22,"jql":"...","truncated":false}}
```

---

## `assigned`

Convenience wrapper over `search`. Defaults to `assignee = currentUser() AND statusCategory != "Done" ORDER BY updated DESC`.

    jiracli show assigned [--state <todo|in-progress|done|all>] [flags]

### Flags

| Flag | Description |
|---|---|
| `--state todo` | `statusCategory = "To Do"` |
| `--state in-progress` | `statusCategory = "In Progress"` |
| `--state done` | `statusCategory = "Done"` |
| `--state all` | No status filter |
| `--limit N` | Results per page (default 50, max 100) |
| `--page N` | 1-indexed page |
| `--json` | NDJSON output |
| `--profile <name>` | Credential profile |
| `--keys-only` | Print one issue key per line (no headers, no footer); ideal for piping |

Output is identical to `search`. The header line shows the effective JQL so the caller can see exactly what was run. The default-open filter is not applied separately — the JQL is pre-built and complete.

---

## `board list`

List Agile boards for a project.

    jiracli board list --project <KEY> [flags]
    jiracli lookup boards --project <KEY> [flags]   # identical alias

### Flags

|Flag|Default|Description|
|---|---|---|
|`--project <KEY>`|—|Project key (**required**)|
|`--type <type>`|—|Filter by board type: `scrum` or `kanban`|
|`--limit N`|50|Results per page (max 100)|
|`--page N`|1|1-indexed page|
|`--json`|false|NDJSON output|
|`--no-cache`|false|Bypass 1h cache|
|`--profile <name>`|default|Credential profile|

Calls `GET /rest/agile/1.0/board?projectKeyOrId=<KEY>`. Cached per project key, TTL 1h (page 1 only with limit ≥ 50).

### Plain-text output

```
  101       Car Release Scrum                         scrum
  102       Car Kanban Board                          kanban

→ jiracli sprint current --board 101
→ jiracli board issues 102
→ jiracli sprint list --board 101
```

### NDJSON output (`--json`)

```ndjson
{"id":101,"name":"Car Release Scrum","type":"scrum"}
{"id":102,"name":"Car Kanban Board","type":"kanban"}
```

Pagination trailer when more pages exist:
```json
{"_pagination":{"page":1,"pages":-1,"total":-1,"next_page":2,"has_more":true}}
```
`pages` and `total` are `-1` because the Agile board endpoint does not report total page count. Use `has_more` as the canonical signal.

---

## `board show <id>`

Show board configuration: type, columns, and column status IDs.

    jiracli board show <id> [flags]

`<id>` must be numeric. For name-based resolution, pass `--project <KEY>`.

### Flags

|Flag|Default|Description|
|---|---|---|
|`--project <KEY>`|—|Required when `<id>` is a board name (not a number)|
|`--json`|false|NDJSON output|
|`--no-cache`|false|Bypass 1h cache|
|`--profile <name>`|default|Credential profile|

### Plain-text output

```
Board 101  Car Release Scrum  scrum

Columns:
  To Do                 statuses: [1, 10269]
  In Progress           statuses: [10020, 3]
  Done                  statuses: [6]

Drill in:
  → jiracli board issues 101
  → jiracli sprint list --board 101
```

Kanban boards omit the `→ jiracli sprint list` line.

### NDJSON output (`--json`)

Single `BoardConfig` object:

```json
{"id":101,"name":"Car Release Scrum","type":"scrum","columns":[{"name":"To Do","statusIds":["1","10269"]},{"name":"In Progress","statusIds":["10020","3"]},{"name":"Done","statusIds":["6"]}]}
```

---

## `board issues <id>`

List all issues on a board via the Agile API.

    jiracli board issues <id> [flags]

### Flags

|Flag|Default|Description|
|---|---|---|
|`--limit N`|50|Results per page (max 100)|
|`--page N`|1|1-indexed page|
|`--json`|false|NDJSON output|
|`--keys-only`|false|Print one key per line|
|`--profile <name>`|default|Credential profile|

Calls `GET /rest/agile/1.0/board/<id>/issue`. Output shape is identical to `search` plain text and NDJSON; header line reads `board: <id>  <name>` instead of a JQL string.

---

## `sprint list`

List sprints for a scrum board.

    jiracli sprint list --board <id> [flags]

### Flags

|Flag|Default|Description|
|---|---|---|
|`--board <id>`|—|Scrum board ID (**required**)|
|`--state <csv>`|_(empty → default view)_|Comma-separated subset: `active`, `future`, `closed`, `all`|
|`--all`|false|Show every sprint (all states, full history); disables the recency window|
|`--closed-within N`|7|Include closed sprints ending within N days (default view only; ignored with `--all`/`--after`/`--before`)|
|`--limit N`|50|Results per page|
|`--page N`|1|1-indexed page|
|`--name-contains <text>`|—|Case-insensitive substring filter on sprint name (client-side)|
|`--after YYYY-MM-DD`|—|Keep sprints whose `endDate` is on/after this date (fetches full history)|
|`--before YYYY-MM-DD`|—|Keep sprints whose `startDate` is on/before this date (fetches full history)|
|`--sort asc\|desc`|`desc` for `--state closed`, else `asc`|Sort order (newest-first for closed)|
|`--no-cache`|false|Bypass local cache|
|`--json`|false|NDJSON output|
|`--profile <name>`|default|Credential profile|

By default (`--state` empty, no `--all`, no date filter) `sprint list` returns only **active + future sprints plus closed sprints that ended within the last 7 days** (`--closed-within N` widens the window). It fetches the full id/name/state set in one GreenHopper `sprintquery` call (cached 1 h), then scans closed sprints newest-first and hydrates their dates only until one falls outside the window — sprint id is a recency proxy, so hydration stops early. Sprints closed > 90 days ago are cached permanently (`archive`) and never refetched. `--all` (and `--state all`) return the complete history; `--after`/`--before` fetch the complete hydrated set (fixing under-reporting of the paged closed endpoint). See `docs/boards-sprints.md` for the full endpoint decision table. On a kanban board: exits 1 with corrective message:

    board 102 is kanban and does not support sprints — use: jiracli board issues 102

### Plain-text output

```
  2001      active    Sprint 42                                  2026-06-15 → 2026-06-29
    → jiracli sprint issues 2001
  2002      future    Sprint 43                                  2026-06-30 → 2026-07-14
    → jiracli sprint issues 2002
```

---

## `sprint show <id>`

Show full details for a sprint.

    jiracli sprint show <id> [flags]

### Flags

|Flag|Default|Description|
|---|---|---|
|`--json`|false|NDJSON output|
|`--profile <name>`|default|Credential profile|

### Plain-text output

```
Sprint 2001  Sprint 42  active
Dates:  2026-06-15 → 2026-06-29  (activated 2026-06-15)
Board:  101
Goal:   Ship login redesign

Drill in:
  → jiracli sprint issues 2001
```

`Goal: (none)` when empty.

### NDJSON output (`--json`)

```json
{"id":2001,"name":"Sprint 42","state":"active","startDate":"2026-06-15T00:00:00.000Z","endDate":"2026-06-29T23:59:59.000Z","activatedDate":"2026-06-15T00:00:00.000Z","originBoardId":101,"goal":"Ship login redesign"}
```

---

## `sprint issues <id>`

List issues in a sprint via the Agile API.

    jiracli sprint issues <id> [flags]

### Flags

|Flag|Default|Description|
|---|---|---|
|`--limit N`|50|Results per page (max 100)|
|`--page N`|1|1-indexed page|
|`--json`|false|NDJSON output|
|`--keys-only`|false|Print one key per line|
|`--profile <name>`|default|Credential profile|

Calls `GET /rest/agile/1.0/sprint/<id>/issue`. Output shape identical to `search`; header reads `sprint: <id>  <name>`.

---

## `sprint current`

Show the active sprint and its issues for a scrum board.

    jiracli sprint current --board <id> [flags]

### Flags

|Flag|Default|Description|
|---|---|---|
|`--board <id>`|—|Scrum board ID (**required**)|
|`--assigned`|false|Show only issues assigned to current user (client-side)|
|`--exclude-done`|false|Exclude issues in Done status category (client-side)|
|`--json`|false|NDJSON output|
|`--profile <name>`|default|Credential profile|

### Behaviour

- **0 active sprints** → exits 1: `no active sprint for board 101 — list options with: jiracli sprint list --board 101 --state future`
- **1 active sprint** → renders sprint detail (`sprint show` format) followed by up to 25 issues (`sprint issues` format). When total > 25, appends `→ jiracli sprint issues 2001 --limit 100`.
- **>1 active sprints** → exits 1, lists each sprint with drill-down hints.
- **Kanban board** → exits 1: `board 102 is kanban and does not support sprints — use: jiracli board issues 102`

### JSON mode

Emits the sprint object first, then one issue NDJSON record per line, then the pagination trailer if applicable.

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

When the issue has no comments, a friendly empty-state line is printed (consistent with `show attachments`) rather than a bare pagination footer:

```
ACME-123 has no comments.
```

With `--since`, the empty-state reads `ACME-123 has no comments on or after <date>.`

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

## `hierarchy` and `effort`

Both are top-level commands (not `show` subcommands) with their own dedicated pages:

- [`hierarchy <KEY>`](hierarchy.md) — maps an issue's place in the work hierarchy, both up the parent chain and down through descendants.
- [`effort`](effort.md) — rolls up time estimates and Story Points across an issue's children (or a JQL/sprint result set).

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

- Invalid ref form: `<input> is not a valid attachment reference — expected <KEY>:attach:<id>`
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

Browser: `open` on macOS, `xdg-open` on Linux. On other OS: prints the URL to stdout with `cannot auto-open on this OS — copy the URL above`.

### Pagination and overflow

All paginated commands (`search`, `comments`, `history`, `assigned`) emit a footer:

- More pages exist: `--- page M of N | next: jiracli <cmd> --page <M+1> [flags] "<args>" ---`
- Last page: `--- page M of N ---`

Outputs exceeding ~200 lines or ~50 KB are truncated; the full content is written to `<tmpdir>/jiracli-output/output-N.txt`, where `<tmpdir>` is the OS temp directory — `/tmp` on Linux, but a per-user `$TMPDIR` path on macOS (not `/tmp`). The truncated output includes hints with the actual resolved path to use `grep`, `head`, or `tail` on that file.
