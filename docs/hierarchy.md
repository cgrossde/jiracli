# jiracli — `hierarchy`

Top-level command. Maps an issue's place in the work hierarchy in **both directions**:

- **Upward** — walks the parent chain (Story → Epic → Initiative/portfolio), so you can see which higher-level item an issue ultimately rolls up into.
- **Downward** — fetches the descendants appropriate to the subject's type, so you can list everything that feeds into an Initiative or Epic (Epics → Stories → sub-tasks).

Point it at a Story to answer *"where does this sit?"*; point it at an Initiative or Epic (with `--depth`) to answer *"what's everything under this?"*.

    jiracli hierarchy <KEY> [flags]

Requires hierarchy field IDs to be configured for the profile — run `jiracli setup --reconfigure` or `jiracli config hierarchy --rediscover` first.

Related: [`effort`](effort.md) rolls up time and Story Points across the same children instead of listing them.

---

## Flags

| Flag | Description |
|---|---|
| `--json` | NDJSON output (one object: the full chain). Honors the filters below and the 100-result cap (`childrenTruncated`/`childrenTotal` report truncation); combine with `--all` to fetch everything |
| `--profile <name>` | Credential profile |
| `--depth N` | Levels of descendants to fetch (default 1 = direct children; max 5) |
| `--exclude-done` | Hide children in the Done status category |
| `--open` | Show only non-Done children (alias for `--exclude-done`) |
| `--state <cat>` | Keep only children in this status category: `todo`, `in-progress`, `done`, `all`. `--state` takes precedence over `--exclude-done`/`--open`; `all` disables filtering |
| `--all` | Fetch all children (bypasses the 100-result default cap) |
| `--flat` | Flat TSV output: one row per node (`depth`, `key`, `type`, `status`, `assignee`, `summary`). With `--json` emits NDJSON flat mode. |
| `--since <date>` | Only include issues updated on or after this date (`-2w`, `-1d`, `2024-01-01`). Bare durations (`2w`) have `-` prepended. |

The `--exclude-done` / `--open` / `--state` filter vocabulary is shared with `jiracli search` and `jiracli effort`. Filtering is applied **server-side** (the status-category predicate is added to the children/sibling/descendant JQL, exactly like `--since`), so `childrenTotal`, `siblingsTotal`, and the truncation flags already reflect the filtered set — and every output mode (plain text, `--flat`, `--json`, `--flat --json`) reports identical results. Inline sub-tasks, which arrive embedded with the subject and are never paginated, are filtered client-side. The subject itself is always shown, even when the active filter would exclude it.

---

## Walk behaviour

- **Ancestor walk**: follows Portfolio → Parent Link → typed `parent` field → Epic Link, up to 8 hops. Cycles are detected and stopped silently.
- **Children**:
  - Subject is an **Epic** → children via JQL `"Epic Link" = KEY` (one search call)
  - Subject is a **portfolio-level type** (Initiative, Programme, Feature, etc.) → children via JQL `"<portfolioFieldName>" = KEY`
  - Otherwise → subtasks from the subject's inline response (no extra call)
- Up to 100 children are returned; Done-last sort within the display cap of 15.

## `--depth` — recursive subtree

`--depth N` fetches N levels of descendants instead of just direct children. Default is 1 (today's behaviour). Maximum is 5.

```
jiracli hierarchy ACME-50 --depth 2
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

When combined with a filter (`--open`, `--exclude-done`, or `--state`), the filter is applied server-side, so Done nodes are simply absent from the tree at every level and the truncation counts reflect the filtered set. There is no "hidden by filter" footer — the tree shows exactly what matched.

## `--flat` — tabular output

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

## `--since` — activity filter

`--since <date>` restricts all fetched children (at every depth level) to issues updated on or after the given date. Combines well with `--depth 2` to show recent activity across an Initiative:

```
jiracli hierarchy ACME-50 --depth 2 --since -2w --open
```

Accepted formats: Jira relative dates (`-2w`, `-1d`, `-30m`) and ISO dates (`2024-01-01`). Bare durations (`2w`) are accepted and have `-` prepended automatically.

---

## Plain-text output shape (depth 1)

```
ACME-50         [Initiative]  Open            Modernise authentication platform
ACME-100        [Epic]        Open            Auth reliability work
  ├─ PROJ-501     [Story]  To Do          Jane Smith              Add OAuth flow
▶ ├─ ACME-123     [Bug]    In Progress    John Doe                Fix login page timeout
  │  ├─ ACME-150   [Sub-task]  To Do      Jane Smith              Reproduce on Safari
  │  └─ ACME-151   [Sub-task]  Done       John Doe                Write regression test
  └─ PROJ-502     [Story]  Done           Jane Smith              Update session tokens

[exit:0 | Xms]
```

Ancestor rows are dimmed (grey) when the terminal supports ANSI. The subject is prefixed with `▶`. Children use `├─` / `└─` tree connectors. When children are capped at 15, a `… N more` line is appended.

When the subject has a parent, its siblings (co-children of the parent) are shown alongside it. The subject is marked with `▶` inline in the sibling tree; its own children expand under it using `│  ` continuation lines. Non-Done siblings come first; the subject always appears first within its done-group. When siblings exceed 100, a `… N more siblings — rerun with --all` line is appended.

When the subject has no ancestors and no children:
```
▶ ACME-999       [Task]        Open            Standalone task
(standalone issue — no parent or children)
```

## NDJSON output (`--json`)

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
  "childrenTotal": 2,
  "siblings": [
    {"key":"PROJ-501","summary":"Add OAuth flow","status":"To Do","statusCategory":"To Do","issueType":"Story","assignee":"Jane Smith"},
    {"key":"ACME-123","summary":"Fix login page timeout","status":"In Progress","statusCategory":"In Progress","issueType":"Bug","assignee":"John Doe","isSubject":true},
    {"key":"PROJ-502","summary":"Update session tokens","status":"Done","statusCategory":"Done","issueType":"Story","assignee":"Jane Smith"}
  ],
  "siblingsTotal": 3
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

## Truncation

When `--depth >= 2` and any level-2+ batch hit the 100-result cap without `--all`, the output appends:
```
   (some subtrees may be incomplete — rerun with --all to fetch every descendant)
```
In JSON mode, `"descendantsTruncated": true` is set on the root object.

Field notes:
- Filtering (`--open`/`--exclude-done`/`--state`) applies to `--json` identically to plain text: the status predicate is pushed into the JQL, so `children`/`siblings` contain only matching nodes and `childrenTotal`/`siblingsTotal` are the filtered server-side counts. `--json` respects the 100-result cap (it does **not** imply `--all`); when more matching children exist, `childrenTruncated` is `true` and `childrenTotal` exceeds `len(children)` — combine with `--all` to fetch the rest.
- `descendantsTruncated`: `true` when `--depth >= 2` and any subtree hit the 100-result cap without `--all`.
- `siblings`: array of sibling nodes (co-children of the nearest ancestor), including the subject with `"isSubject": true`. Omitted (`omitempty`) when the subject has no parent (root issue). Sorted: non-Done first, subject first within its done-group. Capped at 100 by default; use `--all` to fetch all.
- `siblingsTotal`: total server-side sibling count. May exceed `len(siblings)` when capped. Omitted when zero.
- `siblingsTruncated`: `true` when siblings were capped and more exist. Omitted (`omitempty`) when false.

## Errors

- Hierarchy not configured: `hierarchy not configured for profile "default" — run: jiracli setup --reconfigure`, exit 1.
- Non-issue ref: `hierarchy requires a plain issue key — got "ACME-123:comment:9421"`, exit 1.
