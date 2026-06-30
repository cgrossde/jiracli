---
name: jira
description: >
  Read, search, comment on, transition, and create Jira issues.
  Use whenever the user mentions a Jira ticket, asks to search Jira,
  or asks to write/update a ticket.
allowed-tools:
  - Bash
  - Read
---

# /jira

You have `jiracli` on PATH. **The CLI is self-documenting — run `jiracli --help` or `jiracli <command> --help` whenever you are unsure of flags or syntax.** The plain-text output also contains drill-down hints (lines starting with `→`) that show the exact next command to run; follow them rather than guessing.

**Do not use `--json` for normal agent use.** Plain-text output is richer, self-navigating, and easier to reason about. Reserve `--json` only when you need to pass structured data to another tool or script.

---

## High-value workflows

### Auth
```sh
jiracli auth status          # check current profile and whether the PAT is valid
jiracli auth reauth          # re-enter PAT for the active profile
jiracli auth profile --list  # list profiles; set default with: jiracli auth profile <name>
```

### Show
```sh
jiracli show PROJ-123
jiracli show PROJ-123 PROJ-124 PROJ-125          # multiple issues, separated by rules
jiracli search --keys-only --assigned | jiracli show -  # stdin mode: one key per line
jiracli show assigned
jiracli show assigned --category in-progress
jiracli show transitions PROJ-123        # check available statuses before transitioning
jiracli show PROJ-123:attach:10042 -o    # stream attachment to stdout
jiracli show PROJ-123:attach:10042 -f ./file.png  # download to disk
```
The output contains `→` drill-down hints for comments, history, and attachments. Follow them rather than constructing those commands yourself. Attachment IDs appear as `(id: …)` in the attachments section of `jiracli show PROJ-123`.

**Multiple keys / stdin mode**: pass multiple keys as positional args, or pass `-` as the sole argument to read keys from stdin (one per line). Blank lines and `# comment` lines are skipped — so `--keys-only` output pipes in cleanly. In multi-key mode each issue is preceded by a `━━━ KEY (N/M) ━━━` separator; errors on individual keys are printed inline and the loop continues.

### Time estimates & Story Points
```sh
jiracli show PROJ-123           # Estimates: block + progress bar + Story Points line appear automatically when data exists
jiracli show rollup PROJ-100    # aggregate time + SP across all children of an Epic
jiracli show rollup PROJ-100 --all  # page through all children (not just first 100)
jiracli show rollup PROJ-100 --json # structured output: epicKey, total, buckets, unestimated
```
Story Points and the Estimates block require hierarchy fields to be configured. Run `jiracli config hierarchy --json` and check for `storyPointsField` — if missing, run `jiracli config hierarchy --rediscover`.


### Search
```sh
jiracli search "project = PROJ AND priority = High"
jiracli search "project = PROJ" --assigned
jiracli search "project = PROJ" --exclude-done   # hide Done issues
jiracli search --jql 'text ~ "KSP" AND project = CAR'  # --jql for queries with quoted literals
jiracli search --keys-only --assigned             # one key per line — pipe into further commands
jiracli search --keys-only "project = PROJ AND fixVersion = 5.2"
```
All issues are returned by default, **including Done**. Use `--exclude-done` to hide them. The effective JQL is echoed on the first line — read it to confirm the query is what you intended.

**Use `--jql` instead of positional args whenever the JQL contains quoted string literals** (e.g. `text ~ "term"`, `summary ~ "foo bar"`). Shell quoting can mangle the joined positional args; `--jql` passes the entire query as one string and bypasses the join entirely. `--jql` and positional JQL args are mutually exclusive.

**`--keys-only` prints one issue key per line** — no headers, no formatting. Ideal for piping into `jiracli show`, `edit status`, or `xargs`. When more pages exist, the last line is `# next: <cmd>` (filter with `grep -v '^#'` if needed). Also available on `show assigned`.

```sh
# Review piped keys one by one
jiracli search --keys-only --assigned --category in-progress | jiracli show -

# Quick multi-key review
jiracli show CAR-1 CAR-2 CAR-3
```

### Transition, assign, edit fields
```sh
jiracli edit status   PROJ-123 "In Review"                        # single, dry-run
jiracli edit status   PROJ-123 PROJ-124 PROJ-125 Done --yes       # bulk
jiracli edit assignee PROJ-123 me
jiracli edit assignee PROJ-123 PROJ-124 PROJ-125 me --yes         # bulk
jiracli edit field    PROJ-123 "priority=High" "labels+=regression"
jiracli edit field    PROJ-123 PROJ-124 PROJ-125 "labels+=triage" --yes  # bulk
```

**Bulk writes**: all bulk commands (status/assignee/field) share the same conventions — preview lists all planned operations; execution is sequential with per-key error reporting (failures don't abort the rest). Always dry-run first (omit `--yes`), confirm, then re-run with `--yes`.

For `edit field`, leading args without `=` are keys; the first arg containing `=` starts the spec list. Specs are applied identically to every key.

### Add
```sh
jiracli add comment    PROJ-123 "Your comment here."
jiracli add link       PROJ-123 PROJ-456 --type Blocks
jiracli add attachment PROJ-123 screenshot.png
```

### Create an issue
```sh
# Inline
jiracli create --project PROJ --type Bug --summary "..." --assignee me

# Draft workflow — preferred for anything non-trivial
jiracli create --init-draft new-issue.yaml   # generates a YAML template
# edit new-issue.yaml
jiracli create --from-draft new-issue.yaml   # preview with full validation
jiracli create --from-draft new-issue.yaml --yes
```

### Lookup

Jira metadata (components, versions, priorities, link types, assignees) is project-specific — you cannot guess these values. Run `lookup` before any write that names one of them, or the CLI will reject it.

```sh
jiracli lookup components --project PROJ
jiracli lookup priorities --project PROJ
jiracli lookup users "alex" --project PROJ
jiracli lookup link-types
```

More subcommands: run `jiracli lookup --help`.

### Delete
```sh
jiracli delete PROJ-123                          # single issue
jiracli delete PROJ-123 PROJ-124 PROJ-125        # bulk issues
jiracli delete PROJ-123:comment:9421             # single comment
jiracli delete PROJ-123:attach:10042             # single attachment
jiracli delete PROJ-123:link:5860223             # single link
```
The type is inferred from the ref shape. Multi-key only applies when every arg is a bare issue key — compound refs (comment/attach/link) always require a single argument. Link IDs appear as `(id: …)` on each link line in `jiracli show` output. `rm` is an alias for `delete`.

### Open in browser
```sh
jiracli open PROJ-123                    # open issue in browser
jiracli open PROJ-123:comment:9421       # jump to a specific comment
jiracli open PROJ-123 --print-url        # just print the URL
```

There is more — multiple profiles and more. **Run `jiracli --help` and `jiracli <command> --help` to discover them.**

---

## Rules that `--help` won't tell you

**Always preview before applying.** Every write defaults to dry-run. Show the preview to the user and ask "Apply?" before re-running with `--yes`. Never pass `--yes` on the first call unless the user explicitly said "just do it."

**Look up before writing.** See the Lookup section above. The CLI will refuse with a corrective error if a name is invalid — use that error's suggested lookup command.

**Filter on `statusCategory`, not status name.** Status names vary by project. The three universal values are `"To Do"`, `"In Progress"`, `"Done"`. Use these in JQL.

**Truncated output.** Outputs over ~200 lines are written to `/tmp/jiracli-output/output-N.txt` with exploration hints inline. Use `grep`/`head`/`tail` on that file rather than re-running.
