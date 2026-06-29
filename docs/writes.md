# jiracli — Write Commands

Reference for: `add comment`, `edit status`, `edit assignee`, `edit field`, `create`, `add link`, `add attachment`. For deletes, see [delete.md](delete.md).

All write commands accept `--profile <name>` and `--yes`. Most accept `--no-cache`.

---

## Write safety model

Every write command defaults to a dry-run preview:

1. Builds the exact HTTP request.
2. Validates names (component, version, label, priority, assignee, link type, transition) via lookup — cached where available.
3. Prints the preview block and exits 0 **without sending**.

`--yes` skips the preview and applies immediately.

In an interactive TTY (stdin and stdout are a terminal) without `--yes`: after printing the preview, the CLI prompts `Apply? [y/N]:` reading from and writing to `/dev/tty` directly (bypasses stdin/stdout redirection). Answering `y` applies; anything else aborts with exit 0.

If `/dev/tty` is unavailable (CI, container, redirected stdin): no prompt is issued. The preview prints and the command exits 0 without applying. Add `--yes` to apply from scripts.

### Preview block shape

```
DRY RUN — no changes made.

<METHOD> <full URL>

Body:
  <JSON body, 2-space indent, up to 40 lines>

Effect:
  <one-line description of what the request does>

Validation:
  ✓ <check passed>
  ⚠ <warning>
  ✗ <hard failure — blocks apply>

To apply:
  re-run with --yes
[exit:0 | Xms]
```

Any `✗` row in the validation block prevents `--yes` from applying. The error message points at the corrective lookup command.

### Validation refetch

When a name fails validation under `--yes` (e.g., a component that was added after the cache was populated), the CLI automatically invalidates that one cache entry (`project/<KEY>` for components/versions) and retries the validation once before surfacing an error.

---

## `comment <KEY> <body...>`

Adds a comment to an issue.

    jiracli add comment <KEY> <body...> [--file <path>] [--yes] [--profile <name>]

### Flags

| Flag | Description |
|---|---|
| `--file <path>` | Read comment body from file |
| `--yes` | Apply without confirmation |
| `--profile <name>` | Credential profile |

### Body source order

1. Positional arguments joined with spaces.
2. `--file <path>` — reads file content.
3. `-` as the sole positional argument — reads from stdin (`echo "lgtm" | jiracli add comment ACME-123 - --yes`).
4. Stdin when no args and no `--file`.

Body must be non-empty; empty body is a hard error.

### Preview effect line

    + 1 comment on ACME-123 by Alex Chen (u1)

### Success output

    ✓ commented on ACME-123 (id 9421)
      → jiracli show ACME-123

### Errors

- Issue not found: `[stderr] issue ACME-123 not found — check the key or your PAT may lack browse permission on the project`
- 401: `[stderr] PAT in keychain for profile "X" was rejected (HTTP 401) — run: jiracli auth reauth`

---

## `transition <KEY> <name-or-id>`

Moves an issue to a new status via a workflow transition.

    jiracli edit status <KEY> <name-or-id> [--comment <text>] [--yes] [--profile <name>]

### Flags

| Flag | Description |
|---|---|
| `--comment <text>` | Post a comment atomically with the transition |
| `--yes` | Apply without confirmation |
| `--profile <name>` | Credential profile |

### Name-or-id resolution

- **Numeric id**: used directly without lookup.
- **Name string**: fetches `GET /issue/<KEY>/transitions`, matches case-insensitively and exactly. Multiple matches → error listing all candidates.

### Preview effect line

    ACME-123: In Progress → Done

### Success output

    ✓ transitioned ACME-123: In Progress → Done
      → jiracli show ACME-123

### Errors

```
[stderr] no transition matching "dun" on ACME-123.
        Available transitions:
          21  In Review
          31  Done
          51  Block
        List:    jiracli show transitions ACME-123
[exit:1 | Xms]
```

---

## `assign <KEY> <user>`

Assigns or unassigns an issue.

    jiracli edit assignee <KEY> <user-or-id> [--yes] [--profile <name>]

### Flags

| Flag | Description |
|---|---|
| `--yes` | Apply without confirmation |
| `--profile <name>` | Credential profile |

### User resolution

| Value | Behavior |
|---|---|
| `-` | Unassigns the issue (`{"name": null}`) |
| `me` | Resolves to the current user's `name` from the `myself` cache |
| Exact `name` match | Used as-is |
| Partial string | Calls `/user/assignable/search?project=<KEY-prefix>&query=<input>` (falls back to `/user/search` on 404) |

Single match → uses it. Zero matches → error. Multiple matches → error listing candidates (max 8) with `Try:` hints.

### Success output

    ✓ assigned ACME-123 to Alex Chen (u1)
      → jiracli show ACME-123

or, when unassigning:

    ✓ unassigned ACME-123
      → jiracli show ACME-123

### Errors

```
[stderr] could not resolve assignee "sam" — 3 matches:
        u2  Sam Patel        (active)
        u4  Samuel Diaz      (active)
        u9  Samantha Wright  (inactive)
        Try:     jiracli edit assignee ACME-123 u2
        Search:  jiracli lookup users sam --project ACME
[exit:1 | Xms]
```

---

## `field set <KEY> <spec...>`

Universal field mutator. Updates description, labels, components, fixVersions, priority, assignee, and custom fields via `PUT /issue/<KEY>`.

    jiracli edit field <KEY> <spec...> [--allow-new] [--yes] [--no-cache] [--profile <name>]

### Flags

| Flag | Description |
|---|---|
| `--allow-new` | Allow creating new labels/versions (skips client-side validation for those fields) |
| `--yes` | Apply without confirmation |
| `--no-cache` | Bypass local cache |
| `--profile <name>` | Credential profile |

### Spec format

Each `<spec>` token is `name<op>value`. Always quote each spec token — the operators contain shell-special characters.

| Operator | Meaning | Applicable fields |
|---|---|---|
| `=` | Replace / set | All fields |
| `+=` | Add to list | Multi-value only (`labels`, `components`, `fixVersions`, `versions`) |
| `-=` | Remove from list | Multi-value only |

- Field names: human name (`Story Points`) or canonical id (`customfield_10031`). Case-insensitive name lookup backed by `/field` cache.
- Value `@path`: reads value from file (useful for `description=@desc.md`). Only valid with `=`.
- Value `-` on single-value user fields (e.g. `assignee=-`): unassigns.

Multi-value op on a single-value field → hard error: `field "priority" is single-value; use priority=<v>, not priority+=<v>`.

### Validation

| Field | Validation source |
|---|---|
| `priority` | `lookup priorities --project <KEY>` (project scheme, fallback to global) |
| `components` | `lookup components --project <KEY>` (1h cache) |
| `fixVersions` / `versions` | `lookup versions --project <KEY>` (1h cache) |
| `labels` | `lookup labels --project <KEY>` (5-min cache); bypassed with `--allow-new` |
| `assignee` | `/user/assignable/search` |

`--allow-new` skips client-side validation for labels and versions, passing the value to the server as-is. For components, `--allow-new` skips the check but Jira itself may reject: component creation requires admin rights. Server-side rejection is surfaced verbatim with a hint to contact a project admin.

Priorities, link types, and assignees have no `--allow-new` override.

### Preview effect line (one per spec)

    priority: Medium → High
    labels: [old1, old2] → [old1, old2, regression]

### Success output

    ✓ updated ACME-123 (2 field(s))
      → jiracli show ACME-123

### Errors

```
[stderr] label "reggression" is not used in project ACME.
        Did you mean: regression, regression-suite, regr-mobile
        Search:  jiracli lookup labels reggression --project ACME
        Force:   re-run with --allow-new   (uses it as-is)
[exit:1 | Xms]
```

```
[stderr] component "AuthServce" not found in project WEB.
        Search:  jiracli lookup components AuthServce --project WEB
        List:    jiracli lookup components --project WEB
        Force:   re-run with --allow-new   (creates it)
[exit:1 | Xms]
```

```
[stderr] version "4.5.0" does not exist in project WEB.
        Search:  jiracli lookup versions 4.5 --project WEB
        List:    jiracli lookup versions --project WEB --unreleased
        Force:   re-run with --allow-new
[exit:1 | Xms]
```

```
[stderr] unknown priority "High" for project WEB
        Available: 1 - Very High, 2 - High, 3 - Medium, 4 - Low
        Run: jiracli lookup priorities --project WEB
        Did you mean: 1 - Very High, 2 - High?
[exit:1 | Xms]
```

### Examples

```
jiracli edit field ACME-123 "priority=High" "assignee=u1"
jiracli edit field ACME-123 "labels+=regression" "labels-=stale"
jiracli edit field ACME-123 "description=@desc.md"
jiracli edit field ACME-123 "assignee=-"
jiracli edit field ACME-123 "fixVersions+=4.5.0" --allow-new
```

---

## `create`

Creates a new issue. Dry-run by default.

    jiracli create [flags] [--yes]

### Flags

| Flag | Description |
|---|---|
| `--init-draft <path>` | Write a YAML template to this path and exit |
| `--from-draft <path>` | Load field values from a YAML draft file |
| `--project <KEY>` | Project key |
| `--type <Type>` | Issue type name |
| `--summary <text>` | Issue summary |
| `--description <text>` | Issue description |
| `--priority <name>` | Priority |
| `--assignee <user>` | Assignee username or `me` |
| `--component <name>` | Component name (repeatable) |
| `--label <label>` | Label (repeatable) |
| `--fix-version <v>` | Fix version (repeatable) |
| `--custom <name=value>` | Custom field (repeatable) |
| `--allow-new` | Allow new labels/versions (skips client-side validation) |
| `--yes` | Apply without confirmation |
| `--no-cache` | Bypass local cache |
| `--profile <name>` | Credential profile |

### Draft file workflow

```
jiracli create --init-draft new.yaml   # write YAML template
# edit new.yaml
jiracli create --from-draft new.yaml   # preview with full validation block
jiracli create --from-draft new.yaml --yes   # apply
```

`--init-draft` writes a commented YAML template and exits 0:

```yaml
project: ""
type: ""
summary: ""
description: ""
priority: ""
assignee: ""
components: []
labels: []
fixVersions: []
customFields: {}
```

CLI flags override draft entries when both are present.

### Inline mode

`--project`, `--type`, and `--summary` are the minimum required flags. Missing required fields (per the issue type's create metadata) are listed in the preview validation block.

### Validation block (preview)

```
Validation:
  ✓ project WEB exists, current user can create
  ✓ type Bug is valid for project WEB
  ✓ component "AuthService" matched 1 component in WEB
  ✓ priority "High" is in the WEB priority scheme
  ✓ label "regression" is in use in WEB
  ✓ all required fields for Bug in WEB are present
```

`✗` rows (unknown component, missing required field, invalid priority) block apply even with `--yes`.

`⚠` rows (new label without `--allow-new`) warn but do not block.

Required fields are resolved from `GET /issue/createmeta/<KEY>/issuetypes/<typeId>`. Missing required fields are listed by name with their `allowedValues` when available.

### Success output

    ✓ created WEB-815: Login flow times out on slow networks
      → jiracli show WEB-815

### Errors

```
[stderr] component "AuthServce" not found in project WEB.
        Search:  jiracli lookup components AuthServce --project WEB
        List:    jiracli lookup components --project WEB
        Force:   re-run with --allow-new   (creates it)
[exit:1 | Xms]
```

---

## `link <source> <target>`

Creates an issue link between two issues.

    jiracli add link <source> <target> [--type <name>] [--yes] [--no-cache] [--profile <name>]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--type <name>` | `Blocks` | Link type name (resolved via `lookup link-types` cache) |
| `--yes` | false | Apply without confirmation |
| `--no-cache` | false | Bypass local cache |
| `--profile <name>` | default | Credential profile |

`<source>` and `<target>` are issue keys. `--type` is matched case-insensitively against available link types; the direction (`inward`/`outward`) is determined by the link type's definition. With `--type "Blocks"`, `source blocks target`.

Both issues are validated with cheap `GET /issue/<KEY>?fields=key` calls (run in parallel).

### Preview effect line

    ACME-123 blocks ACME-456

### Success output

    ✓ linked ACME-123 blocks ACME-456

### Errors

```
[stderr] link type "Causes" not found on this instance.
        List:    jiracli lookup link-types
[exit:1 | Xms]
```

---

## `attach <KEY> <file...>`

Uploads one or more files as attachments via multipart POST to `/issue/<KEY>/attachments` with `X-Atlassian-Token: no-check`.

    jiracli add attachment <KEY> <file...> [--yes] [--profile <name>]

### Flags

| Flag | Description |
|---|---|
| `--yes` | Apply without confirmation |
| `--profile <name>` | Credential profile |

At least two arguments required: `<KEY>` and one or more file paths.

### Validation

- Each file path must exist and be readable.
- Total upload size is checked against the instance attachment limit (from `serverInfo` or fallback 100 MB). When the total exceeds 100 MB, a `⚠` row appears in the preview.

### Preview body section

Binary contents are not dumped. The preview shows file list and total size:

```
Files: screenshot.png (142 KB), logcat.txt (9 KB) — 151 KB total
```

### Success output

One line per uploaded file, then a drill-down:

    ✓ attached screenshot.png as ACME-123:attach:11003 (142 KB)
    ✓ attached logcat.txt as ACME-123:attach:11004 (9 KB)
      → jiracli show ACME-123

### Errors

- File not found: `[stderr] file "screenshot.png" not found or not readable`
- Issue not found: `[stderr] issue ACME-123 not found`
- 403 (attachment upload requires project write permission): server error surfaced verbatim.

