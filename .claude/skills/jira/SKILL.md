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
jiracli show assigned
jiracli show assigned --category in-progress
jiracli show transitions PROJ-123        # check available statuses before transitioning
jiracli show PROJ-123:attach:10042 -o    # stream attachment to stdout
jiracli show PROJ-123:attach:10042 -f ./file.png  # download to disk
```
The output contains `→` drill-down hints for comments, history, and attachments. Follow them rather than constructing those commands yourself. Attachment IDs appear as `(id: …)` in the attachments section of `jiracli show PROJ-123`.

### Search
```sh
jiracli search "project = PROJ AND priority = High"
jiracli search "project = PROJ" --assigned
jiracli search "project = PROJ" --exclude-done   # hide Done issues
```
All issues are returned by default, **including Done**. Use `--exclude-done` to hide them. The effective JQL is echoed on the first line — read it to confirm the query is what you intended.

### Transition, assign, edit fields
```sh
jiracli edit status   PROJ-123 "In Review"    # dry-run first
jiracli edit assignee PROJ-123 me
jiracli edit field    PROJ-123 "priority=High" "labels+=regression"
```

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
jiracli delete PROJ-123:comment:9421
jiracli delete PROJ-123:attach:10042
jiracli delete PROJ-123:link:5860223
jiracli delete PROJ-123
```
The type is inferred from the ref shape. Link IDs appear as `(id: …)` on each link line in `jiracli show` output with a copy-paste `→ jiracli delete PROJ-123:link:NNN` hint. `rm` is an alias for `delete`.

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
