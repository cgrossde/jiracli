# jiracli — Lookup Commands

Reference for all 10 `lookup` subcommands. For `cache list` and `cache clear`, see [cache.md](cache.md).

All `lookup` subcommands accept `--profile <name>`, `--json`, and `--no-cache`.

---

## Overview

`lookup` exposes Jira's project metadata — components, versions, users, labels, fields, priorities, link types, statuses, issue types, and projects — that is otherwise inaccessible without reading individual issues. Agents must use `lookup` before any write that names one of these values; unknown names cause hard errors under `--yes`.

Results are served from a disk cache at `~/.cache/jiracli/<profile-hash>/`. Cache keys use slash notation; TTLs vary by resource (see table below).

### Cache TTLs

| Resource | Cache key | TTL |
|---|---|---|
| `myself` | `myself` | 24h |
| Fields (all) | `fields` | 24h |
| Projects (list) | `projects` | 24h |
| Project detail (components, versions) | `project/<KEY>` | 1h |
| Create metadata | `createmeta/<KEY>/<typeID>` | 24h |
| Link types | `linktypes` | 24h |
| Global priorities | `priorities` | 24h |
| Project priority scheme | `project/<KEY>/priorityscheme` | 24h |
| Project label aggregation | `labels/<KEY>` | 5 min |
| Statuses | `statuses` | 24h |
| Global issue types | `issuetypes` | 24h |
| Project issue types | `issuetypes/<KEY>` | 24h |
| User search | not cached | — |

---

## `lookup users [<query>]`

Searches for Jira users by display name, username, or email.

    jiracli lookup users [<query>] [flags]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--project <KEY>` | — | Restrict to users assignable to this project |
| `--active` | false | Return only active users (no-project mode only) |
| `--limit N` | 20 | Maximum results |
| `--no-cache` | false | Skip cache (user search is never cached) |
| `--json` | false | NDJSON output |

Without `--project`: calls `/user/search?query=<q>&maxResults=N`.
With `--project`: calls `/user/assignable/search?project=<KEY>&query=<q>&maxResults=N`.

The Jira DC `query` parameter is a hint — the server may return a broader set regardless of its value. Results are always filtered client-side as a case-insensitive substring match against `username`, `displayName`, and `emailAddress`. Omitting `<query>` returns up to `--limit` users unfiltered.

Same resolution engine used by `assign`.

### Plain-text output shape

Columns: `<username>  <displayName>  <emailAddress>`. One row per user. `no users found` when empty.

### NDJSON output (`--json`)

```ndjson
{"name":"u1","displayName":"Alex Chen","emailAddress":"alex@example.com","active":true}
```

---

## `lookup labels [<query>]`

Two distinct modes depending on whether `--project` is set.

    jiracli lookup labels [<query>] [--project <KEY>] [flags]

### Flags

| Flag | Description |
|---|---|
| `--project <KEY>` | Aggregate labels from this project (5-min cache) |
| `--no-cache` | Bypass cache |
| `--json` | NDJSON output |

### Without `--project` — global autocomplete

Calls `GET /jql/autocompletedata/suggestions?fieldName=labels&fieldValue=<query>`.

- Server hard cap: 15 results. No workaround exists.
- When exactly 15 results are returned, output appends: `(results may be truncated — server limit is 15)`
- Use for: "does this label exist anywhere on the instance?"

### With `--project <KEY>` — JQL aggregation

Pages through `project = <KEY> AND labels IS NOT EMPTY`, collects all `labels[]` values, deduplicates, prefix-filters by `<query>` in Go. Cached at `labels/<KEY>` for 5 minutes.

**This can take up to 60 seconds for large projects** on the first call (before the cache is warm). Progress is not shown. Subsequent calls within 5 minutes return instantly from cache.

Output appends: `Sourced from project <KEY> (cached 5 min)`

Use for: "what labels does this project actually use?"

The `create` preview uses the project-scoped variant. New labels surface as `⚠ label "X" is new in this project (no prior usage) — typo?` in the validation block.

### NDJSON output (`--json`)

```ndjson
{"label":"regression"}
```

---

## `lookup components [<query>]`

Lists components for a project.

    jiracli lookup components [<query>] [--project <KEY>] [flags]

### Flags

| Flag | Description |
|---|---|
| `--project <KEY>` | Project key (**required**) |
| `--no-cache` | Bypass 1h project cache |
| `--json` | NDJSON output |

`--project` is required. Without it: `[stderr] --project required — run: jiracli lookup projects`

Reads `GET /project/<KEY>`, filters `.components[]` by name prefix in Go.

### Plain-text output shape

```
WEB — 2 components matching "auth":

  AuthService  Lead: Alex Chen (u1)  Description: SSO + token rotation
  → jiracli search 'project = WEB AND component = "AuthService"'

  AuthDashboard  Lead: —  Description: Internal admin UI
  → jiracli search 'project = WEB AND component = "AuthDashboard"'

→ jiracli lookup components --project WEB   # full component list
```

### NDJSON output (`--json`)

```ndjson
{"id":"10101","name":"AuthService","lead":{"name":"u1","displayName":"Alex Chen"},"description":"SSO + token rotation","project":"WEB"}
```

---

## `lookup versions [<query>]`

Lists versions (fix versions / releases) for a project.

    jiracli lookup versions [<query>] [--project <KEY>] [flags]

### Flags

| Flag | Description |
|---|---|
| `--project <KEY>` | Project key (**required**) |
| `--released` | Show only released versions |
| `--unreleased` | Show only unreleased versions |
| `--archived` | Include archived versions (hidden by default) |
| `--limit N` | Maximum number of versions to show (0 = all, default: all) |
| `--no-cache` | Bypass 1h project cache |
| `--json` | NDJSON output |

`--project` is required. Reads `GET /project/<KEY>`, filters `.versions[]` in Go. Archived versions are hidden unless `--archived` is set. `--released` and `--unreleased` are mutually exclusive filters.

### Plain-text output shape

One version per line: `<name>  <released|unreleased> [(archived)]  <description>`

### NDJSON output (`--json`)

```ndjson
{"id":"10201","name":"4.5.0","released":false,"archived":false,"description":""}
```

---

## `lookup projects [<query>]`

Lists all visible Jira projects, with optional prefix filter.

    jiracli lookup projects [<query>] [flags]

### Flags

| Flag | Description |
|---|---|
| `--no-cache` | Bypass 24h cache |
| `--json` | NDJSON output |

Calls `GET /project` (returns full visible list; server has no query parameter). Prefix-filters by key or name in Go. First call on a large instance may be slow; subsequent calls hit the 24h cache.

### Plain-text output shape

```
WEB            Web Platform
  → jiracli search "project = WEB"

API            API Gateway
  → jiracli search "project = API"
```

### NDJSON output (`--json`)

```ndjson
{"id":"10001","key":"WEB","name":"Web Platform"}
```

---

## `lookup issue-types`

Lists issue types — global or scoped to a project.

    jiracli lookup issue-types [--project <KEY>] [flags]

### Flags

| Flag | Description |
|---|---|
| `--project <KEY>` | List only types valid for this project |
| `--no-cache` | Bypass 24h cache |
| `--json` | NDJSON output |

Without `--project`: calls `/issuetype` (global list — every defined type, not necessarily usable in any given project).
With `--project`: calls `/issue/createmeta/<KEY>/issuetypes` (the truth for that project).

Per-project is preferred for `create` validation. Use global list for discovery.

### Plain-text output shape

One type per line: `<name>  [subtask]  <description>`

### NDJSON output (`--json`)

```ndjson
{"id":"1","name":"Bug","description":"A problem or error","subtask":false}
```

---

## `lookup link-types`

Lists all issue link types on the instance.

    jiracli lookup link-types [flags]

### Flags

| Flag | Description |
|---|---|
| `--no-cache` | Bypass 24h cache |
| `--json` | NDJSON output |

Calls `/issueLinkType`. Instance-global. Feeds `link --type` name resolution.

### Plain-text output shape

```
Blocks                     (inward: is blocked by / outward: blocks)
Relates                    (inward: relates to / outward: relates to)
Duplicates                 (inward: is duplicated by / outward: duplicates)
```

### NDJSON output (`--json`)

```ndjson
{"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks"}
```

### Errors

- Unknown type in `link --type` → `[stderr] link type "Causes" not found on this instance. List: jiracli lookup link-types`

---

## `lookup statuses`

Lists all workflow statuses with their `statusCategory`.

    jiracli lookup statuses [--query <q>] [flags]

### Flags

| Flag | Description |
|---|---|
| `--query <q>` | Filter statuses by name prefix |
| `--no-cache` | Bypass 24h cache |
| `--json` | NDJSON output |

Calls `GET /status`. Exists for discovery. Agents should always filter by `statusCategory` (not status name) in JQL — status names vary per project and workflow.

The three universal `statusCategory` values: `"To Do"`, `"In Progress"`, `"Done"`.

### Plain-text output shape

```
Open                           [To Do]
In Progress                    [In Progress]
In Review                      [In Progress]
Done                           [Done]
Closed                         [Done]
```

### NDJSON output (`--json`)

```ndjson
{"id":"1","name":"Open","statusCategory":"To Do"}
```

---

## `lookup priorities`

Lists priorities — global or per-project priority scheme.

    jiracli lookup priorities [--project <KEY>] [flags]

### Flags

| Flag | Description |
|---|---|
| `--project <KEY>` | Resolve the priority scheme for this project (DC 7.7+) |
| `--no-cache` | Bypass 24h cache |
| `--json` | NDJSON output |

Without `--project`: calls `GET /priority` (global list).
With `--project`: resolves via `/project/<KEY>/priorityscheme` → `/priorityscheme/{id}/priorities`. On 404 from the scheme endpoint (DC < 7.7 or feature not enabled): falls back to the global list and appends:

    (note: no per-project priority scheme — showing global list)

In `--json` mode the fallback is signalled by a trailing `{"note":"no per-project priority scheme — showing global list"}` record.

### Plain-text output shape

One priority name per line. The list is ordered as the scheme defines.

### NDJSON output (`--json`)

```ndjson
{"id":"1","name":"High"}
```

### Errors

- Unknown priority in `field set` or `create` → `[stderr] priority "Critical" is not in the WEB priority scheme. Available: High, Medium, Low. List: jiracli lookup priorities --project WEB`

---

## `lookup fields [<query>]`

Lists or resolves Jira fields (system and custom).

    jiracli lookup fields [<query>] [flags]
    jiracli lookup fields --id <name-or-id> [--project <KEY> --type <typeId>]

### Flags

| Flag | Description |
|---|---|
| `--custom` | Show only custom fields (`customfield_*`) |
| `--id <name-or-id>` | Resolve a single field by name or canonical id |
| `--project <KEY>` | Used with `--id` and `--type` to fetch `allowedValues` |
| `--type <typeId>` | Issue type id (used with `--id` and `--project`) |
| `--no-cache` | Bypass 24h cache |
| `--json` | NDJSON output |

Calls `GET /field`. Cached per-profile for 24h.

Positional `<query>` prefix-filters by field name or id. `--custom` further narrows to `customfield_*` only.

### `--id` mode

Resolves a single field by name (case-insensitive) or canonical id. Returns the field's id, name, schema type, and (when `--project` + `--type` are also set) the `allowedValues` list from the create metadata for that project/type combination.

### Plain-text output shape (list mode)

```
summary                              Summary                            string
description                          Description                        string
customfield_10031                    Story Points                        [custom]  number
customfield_10050                    Team                                [custom]  string
```

### Plain-text output shape (`--id` mode)

```
id:     customfield_10031
name:   Story Points
custom: true
type:   number
```

With `--project` + `--type`:

```
allowedValues:
  Sprint 1 (10100)
  Sprint 2 (10101)
```

### NDJSON output (`--json`, list mode)

```ndjson
{"id":"customfield_10031","name":"Story Points","custom":true,"type":"number"}
```

### NDJSON output (`--json`, `--id` mode)

```ndjson
{"id":"customfield_10031","name":"Story Points","custom":true,"type":"number","allowedValues":[...]}
```

`allowedValues` is omitted when not requested.

