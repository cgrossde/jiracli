---
name: jira
description: >
  Read, search, comment on, transition, and create Jira issues. Also boards,
  sprints, and sprint membership. Use whenever the user mentions a Jira ticket,
  asks to search Jira, or asks to write/update a ticket.
allowed-tools:
  - Bash
  - Read
---

# /jira

You have `jiracli` on PATH. **The CLI is self-documenting — run `jiracli --help` or `jiracli <command> --help` whenever you are unsure of flags or syntax.** Plain-text output contains `→` drill-down hints showing the exact next command to run; follow them rather than guessing.

**Do not use `--json` for normal agent use.** Plain-text output is richer and self-navigating. Reserve `--json` for passing structured data to another tool or script.

---

## High-value workflows

### Auth
```sh
jiracli auth status          # check current profile and PAT validity
jiracli auth profile --list  # list profiles; set default with: jiracli auth profile <name>
```
Run `jiracli auth --help` for more.

### Read an issue
```sh
jiracli show PROJ-123
jiracli show PROJ-123 PROJ-124 PROJ-125    # multiple issues with separators
jiracli show assigned                       # issues assigned to you
jiracli show transitions PROJ-123           # available statuses before transitioning
jiracli show comments PROJ-123             # full comment thread
jiracli show history PROJ-123              # changelog
jiracli show PROJ-123 --parent             # show the immediate parent (one level only)
jiracli hierarchy PROJ-123                 # locate up (Story → Epic → Initiative) + list children down (add --depth N)
jiracli open PROJ-123 --print-url          # get the browse URL without opening a browser
```
Run `jiracli show --help` for all flags and subcommands (`--no-history`, `--no-comments`, `--fields`, and more).

**`show KEY1 KEY2 … KEY-N` is serial and slow.** Each key is a separate API call. For status-only lookups across many keys, prefer a single `search` call instead — it is one round-trip regardless of result count:
```sh
# Slow — N serial API calls
jiracli show PROJ-1 PROJ-2 PROJ-3 ... PROJ-20

# Fast — one API call, same status info
jiracli search --jql 'key in (PROJ-1, PROJ-2, PROJ-3, ..., PROJ-20)' --limit 100
```
`search` isn't limited to status — pass `--fields` to render whatever columns you need, so a single call can return the same fields you'd otherwise batch-`show` for:
```sh
# Add assignee + priority columns to the result
jiracli search --jql 'key in (PROJ-1, PROJ-2, ...)' --fields '+assignee,+priority' --limit 100

# Fetch exactly the fields you want (replaces the default columns)
jiracli search --jql 'key in (PROJ-1, PROJ-2, ...)' --fields-only 'key,summary,status,assignee' --limit 100
```
Use `+name`/`-name` with `--fields` to add or drop columns, or `--fields-only` to specify the exact set. Run `jiracli lookup fields` for available field IDs.

Reserve multi-key `show` for when you need the full issue detail (description, comments, history) on a small set (≤5 keys).

### Search
```sh
jiracli search "project = PROJ AND priority = High"
jiracli search "project = PROJ" --exclude-done                   # or --open (alias)
jiracli search --jql 'text ~ "some phrase" AND project = PROJ'   # use --jql for quoted literals
jiracli search --keys-only --assigned | jiracli show -            # pipe keys into show
```
Bare JQL / `--jql` queries return all issues by default **including Done** — use `--exclude-done` (alias `--open`) to hide them. **`--assigned` is the exception: on its own it excludes Done** — add `--state all` to include Done. The effective JQL is echoed on the first output line; read it to confirm the query.

**Use `--jql` whenever the JQL contains quoted string literals** (`text ~ "term"`, `summary ~ "foo bar"`). Shell quoting mangles positional args; `--jql` passes the whole query as one string.

**`--keys-only` prints one key per line** — pipe into `jiracli show -`, `edit status`, or `xargs`. Also available on `show assigned`.

Run `jiracli search --help` for pagination, field selection, and more.

### Bulk operations
The standard agent pattern for operating on a set of issues:
```sh
# Triage: read all assigned issues in full
jiracli show assigned --keys-only | jiracli show -

# Bulk transition matching issues
jiracli search --keys-only --jql "project = PROJ AND sprint in openSprints() AND assignee = currentUser()" \
  | xargs jiracli edit status Done --yes

# Any edit command accepts multiple keys before the value
jiracli edit status   PROJ-1 PROJ-2 PROJ-3 "In Review"   # dry-run
jiracli edit assignee PROJ-1 PROJ-2 PROJ-3 me --yes
```

### Transition, assign, edit fields
```sh
jiracli edit status   PROJ-123 "In Review"                   # dry-run first
jiracli edit status   PROJ-123 PROJ-124 Done --yes           # bulk apply
jiracli edit assignee PROJ-123 me
jiracli edit field    PROJ-123 "priority=High" "labels+=regression"
```
All edits default to dry-run; pass `--yes` to apply. Bulk: list all keys before the value. Run `jiracli edit --help` for all subcommands.

### Create an issue
```sh
jiracli create --project PROJ --type Bug --summary "..." --yes

# Draft workflow — preferred for anything non-trivial
jiracli create --init-draft new-issue.yaml   # generates a template
jiracli create --from-draft new-issue.yaml   # preview
jiracli create --from-draft new-issue.yaml --yes
```
Run `jiracli create --help` for all fields.

### Hierarchy & effort
Both are top-level commands (not under `show`). They share a filter vocabulary with `search`: `--exclude-done` / `--open` (alias) / `--state todo|in-progress|done|all`. `hierarchy` and `effort` additionally accept `--since` (`search` does not).

`hierarchy` maps an issue **both ways**: **up** the parent chain (Story → Epic → Initiative/portfolio) to locate where it sits, and **down** through descendants (with `--depth`) to list everything nested under an Initiative or Epic.
```sh
jiracli hierarchy PROJ-123            # locate: full chain Story → Epic → Initiative + direct children
jiracli hierarchy INIT-42 --depth 3   # list everything under an initiative (epics → stories → sub-tasks)
jiracli hierarchy PROJ-123 --open     # non-Done issues only (alias for --exclude-done)
jiracli hierarchy PROJ-123 --state in-progress  # only In Progress nodes
jiracli hierarchy PROJ-123 --depth 2  # two levels of descendants
jiracli effort    PROJ-123            # aggregate time + story points across children (totals only)
jiracli effort    PROJ-123 --depth 2  # include grandchildren
jiracli effort    PROJ-123 --limit 500  # fetch up to 500 children (default cap is 100)
jiracli effort    PROJ-123 --all      # fetch all children, no cap
jiracli effort    PROJ-123 --group-by status            # direct children by status (1 table)
jiracli effort    PROJ-123 --group-by status --depth 2  # + grandchildren by status (2 tables)
# jql / sprint subcommands — flat aggregation over a result set, no hierarchy config needed
jiracli effort sprint 12345 --group-by assignee   # time breakdown per person
jiracli effort sprint 12345                        # single Total row
jiracli effort jql 'project = PROJ AND updated >= "2026-04-01"'
```
`effort <KEY>` works on any issue type — Epic aggregates its Stories; Initiative/Portfolio aggregates its Epics. It reports **totals only**; for a per-child breakdown use `jiracli hierarchy KEY`. Hierarchy mode requires hierarchy fields to be configured (`jiracli setup`); the `jql`/`sprint` subcommands do not. Run `jiracli effort --help`, `jiracli effort jql --help`, `jiracli effort sprint --help` for more.

**Three modes, split into subcommands because they are genuinely different:** `effort <KEY>` walks a hierarchy; `effort jql '<query>'` and `effort sprint <id>` aggregate flatly over a result set (and support `--group-by assignee`, which hierarchy mode does not).

**For full downward traversal (initiative → all epics → all stories), use `jiracli hierarchy KEY --depth N --state open|done|all`.** It returns a tree with status per node — one call gives both the structure and per-child status.

**`effort jql` is the primary aggregation primitive for manager-level queries.** When asked for time spent, remaining, or budget health across any slice of work — a quarter, a label, a team, a fix version — reach for this first. It is a single call that replaces many individual effort lookups:
```sh
# Budget across all epics targeting a fix version (e.g. a quarter's commitment)
jiracli effort jql 'issueType = Epic AND fixVersion = "v2026-Q2"'

# Counts and time per status across a quarter's commitment
jiracli effort jql 'issueType = Epic AND fixVersion = "v2026-Q2"' --group-by status

# Coarser breakdown — only To Do / In Progress / Done
jiracli effort jql 'project = PROJ AND ...' --group-by statusCategory

# Budget per assignee across a sprint
jiracli effort sprint 12345 --group-by assignee

# Status breakdown for an initiative's direct children
jiracli effort INIT-123 --group-by status
```
Combine with a separate `search --jql '... AND statusCategory = Done' --keys-only` count to get completion percentage — effort gives you time, search gives you issue counts by status.

### Aggregating search results

When you want counts and percentages rather than an issue list, use `--count-by`:

```sh
# How many epics are in each status for a fixVersion
jiracli search --jql 'issueType = Epic AND fixVersion = "v2026-Q2"' --count-by status

# Bug volume by priority across an open sprint
jiracli search --jql 'project = PROJ AND sprint in openSprints() AND issuetype = Bug' --count-by priority

# Workload distribution across the team
jiracli search --jql 'project = PROJ AND sprint in openSprints()' --count-by assignee
```

Supported fields: `status`, `statusCategory`, `priority`, `assignee`, `issueType`, `resolution`, `project`. `--count-by` paginates internally — no `--limit` needed for scoped queries. For broad/unscoped queries matching more than 500 issues, it aborts with a corrective error rather than silently running for minutes; re-run with `--all` to count every match, or `--limit N` to cap the count and get a clearly-marked partial result. For time totals (Planned/Spent/Remaining) instead of pure counts, use `effort --group-by status` instead.

### Lookup metadata
Jira metadata (components, versions, priorities, link types, users) is project-specific — never guess. Run `lookup` before any write that names one of them.
```sh
jiracli lookup users "alex" --project PROJ
jiracli lookup components --project PROJ
jiracli lookup link-types
```
Run `jiracli lookup --help` to see all subcommands.

### Boards & Sprints
```sh
jiracli board list --project PROJ          # boards for a project
jiracli board show 1234                    # columns and board type
jiracli board show 1234 --details          # + filter name, owner, JQL
jiracli sprint list --board 1234           # default: active + future + closed in last 7d
jiracli sprint list --board 1234 --all     # every sprint (full history)
jiracli sprint current --board 1234        # active sprint with embedded issue list
jiracli sprint issues 5678                 # issues in a specific sprint
jiracli edit sprint PROJ-123 current --board 1234  # move issue to current sprint (dry-run)
jiracli edit sprint PROJ-123 backlog               # remove from sprint
```
If `sprint current` reports multiple active sprints, pass `--sprint <id>` to pick one explicitly. Run `jiracli sprint --help` and `jiracli board --help` for more.

### Add & delete
```sh
jiracli add comment PROJ-123 "comment text"
jiracli add link    PROJ-123 PROJ-456 --type "is related to"
jiracli delete PROJ-123                    # issue (dry-run; add --yes to apply)
jiracli delete PROJ-123:comment:9421       # comment — id from jiracli show output
jiracli delete PROJ-123:link:5860223       # link — id from jiracli show output
```
Run `jiracli add --help` and `jiracli delete --help` for more.

---

## Rules that `--help` won't tell you

**Always preview before applying.** Every write defaults to dry-run. Show the preview and ask "Apply?" before re-running with `--yes`. Never pass `--yes` on the first call unless the user explicitly said so.

**Look up before writing.** Metadata names (components, priorities, users, link types) are project-specific. The CLI will refuse with a corrective error if a name is wrong — use that error's suggested `lookup` command.

**Filter on `statusCategory`, not status name.** Status names vary by project. The three universal values are `"To Do"`, `"In Progress"`, `"Done"`. Use these in JQL.

**Truncated output** (>200 lines) is written to `<tmpdir>/jiracli-output/output-N.txt` with exploration hints inline (`<tmpdir>` is the OS temp dir — `/tmp` on Linux, `$TMPDIR` on macOS; the printed hint always shows the real path). Use `grep`/`head`/`tail` on that file rather than re-running.

**When in doubt, run `--help`.** `jiracli --help`, `jiracli <command> --help`, and `jiracli <command> <subcommand> --help` are always up to date and cover every flag. The skill covers the most common cases; help covers everything.

**Use `hierarchy` to find where a ticket sits in the hierarchy.** `--parent` only walks one level. When asked which epic, initiative, portfolio item, or any higher-level parent a ticket belongs to — or when mapping a set of tickets up the hierarchy — use `jiracli hierarchy PROJ-123`. It returns the full chain (e.g. Story → Epic → Initiative/Portfolio) in a single call. Do not chain `--parent` calls.

**`effort --group-by` vs `search --count-by`:** use `effort ... --group-by status` when you need time estimates (Planned/Remaining/Spent) broken down by status — via a hierarchy `effort <KEY>` or a result set `effort jql '<query>'`. Use `search --count-by` when you only need counts and percentages and don't need time data. `effort jql` and `search --jql` both take a query; `effort <KEY>` additionally accepts `--depth 2` for two-level breakdowns.
