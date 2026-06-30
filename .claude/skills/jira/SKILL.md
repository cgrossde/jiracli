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
jiracli show PROJ-123 PROJ-124 PROJ-125   # multiple issues with separators
jiracli show assigned                      # issues assigned to you
jiracli show transitions PROJ-123          # available statuses before transitioning
```
Run `jiracli show --help` for flags (`--no-history`, `--comments N`, `--parent`, `--fields`, rollup, hierarchy, and more).

### Search
```sh
jiracli search "project = PROJ AND priority = High"
jiracli search "project = PROJ" --assigned --exclude-done
jiracli search --jql 'text ~ "some phrase" AND project = PROJ'   # use --jql for quoted literals
jiracli search --keys-only --assigned | jiracli show -            # pipe keys into show
```
All issues are returned by default **including Done** — use `--exclude-done` to hide them. The effective JQL is echoed on the first output line; read it to confirm the query.

**Use `--jql` whenever the JQL contains quoted string literals** (`text ~ "term"`, `summary ~ "foo bar"`). Shell quoting mangles positional args; `--jql` passes the whole query as one string.

**`--keys-only` prints one key per line** — pipe into `jiracli show -`, `edit status`, or `xargs`. Also available on `show assigned`.

Run `jiracli search --help` for pagination, field selection, and more.

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
jiracli sprint list --board 1234           # active + future sprints
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

**Truncated output** (>200 lines) is written to `/tmp/jiracli-output/output-N.txt` with exploration hints inline. Use `grep`/`head`/`tail` on that file rather than re-running.

**When in doubt, run `--help`.** `jiracli --help`, `jiracli <command> --help`, and `jiracli <command> <subcommand> --help` are always up to date and cover every flag. The skill covers the most common cases; help covers everything.
