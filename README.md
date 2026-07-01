# jiracli

**Scriptable, agent-friendly Jira access from the terminal.**

Read issues, run JQL searches, transition statuses, add comments, create tickets — all with a consistent output format that works for humans, shell scripts, and LLM agents alike.

macOS · Jira

---

```sh
$ jiracli show PROJ-1234

PROJ-1234  In Progress · Bug · High
"Login redirect fails on slow networks"

Assignee: Alex Chen (alex)          Reporter: Sam Patel
Created:  2026-05-10                Updated: 2026-06-22

Description:
  Reproduced on iOS and Android. Timeout fires before the redirect
  completes when round-trip > 800ms.

Activity (newest 10):
  2026-06-22 08:14  Alex Chen   status: Open → In Progress
  2026-06-20 14:30  Sam Patel   fixVersion: 10.5 → 10.6

Drill in:
  → jiracli show comments     PROJ-1234
  → jiracli show history      PROJ-1234
  → jiracli show transitions  PROJ-1234

[exit:0 | 312ms]
```

---

## Why jiracli

Most Jira CLIs are wrappers around the REST API that return raw JSON, leaving you to write `jq` pipelines for every query. jiracli takes the opposite approach:

- **Human-readable by default.** Plain text with a consistent structure — status, activity, links, children — rendered in one command.
- **`--json` for scripts.** Every command has a stable NDJSON output (one object per line) with a v1 schema contract. No silent breaking changes.
- **Built for agents.** Every output is context-window-safe: results over ~200 lines are automatically truncated to a temp file, with ready-to-run `grep`/`tail` commands in the output. Pagination surfaces as copy-paste commands, not page numbers to guess.
- **Dry-run by default on all writes.** Every mutation shows the exact HTTP request, a validation block, and the effect — before touching anything. Pass `--yes` to apply.
- **Cached metadata.** Components, versions, priorities, fields, and link types are cached locally (5 min to 24 h TTLs), so repeated lookups are instant and writes validate without extra round-trips.

---

## Install

```sh
go install github.com/cgrossde/jiracli@latest
```

Requires Go 1.21+. Authenticates with a **Jira Personal Access Token (PAT)**; credentials are stored in the macOS system Keychain — no config files with tokens in plaintext.

## Setup

```sh
jiracli setup
```

The interactive wizard prompts for your Jira server URL and a Personal Access Token, verifies both, and optionally installs the embedded Claude skill. Takes about 30 seconds.

Non-interactive / CI:

```sh
JIRACLI_PAT=<token> jiracli auth login --url https://jira.example.com
```

Verify it worked:

```sh
jiracli auth status
```

Full reference: **[`docs/`](docs/README.md)** — setup, read commands, writes, deletes, lookup, cache, and NDJSON schemas.

---

## Usage

### Reading issues

```sh
jiracli show PROJ-123                          # full issue with activity (inlines latest comment)
jiracli show PROJ-123 --no-history             # skip changelog
jiracli show PROJ-123 --no-comments            # skip the comment preview
jiracli show PROJ-123 --fields "description"  # add description to the default view

jiracli show comments PROJ-123                 # full comment thread
jiracli show history PROJ-123 --since 7d       # changelog, last 7 days
jiracli show transitions PROJ-123              # what statuses are available next
jiracli show attachments PROJ-123              # list attachments
jiracli show PROJ-123:attach:10042 -o          # stream attachment to stdout
```

### Searching

```sh
jiracli show assigned                                        # open issues assigned to you
jiracli show assigned --state in-progress                    # narrow by status category

jiracli search "project = PROJ AND priority = High"
jiracli search "project = PROJ" --assigned --limit 20
jiracli search "project = PROJ AND issuetype = Bug" --state done
```

The effective JQL is echoed on the first line. The default filter excludes `Done` issues — pass `--include-done` to lift it.

### Writing (dry-run by default)

```sh
# Preview first (no --yes)
jiracli edit status   PROJ-123 "In Review"
jiracli edit assignee PROJ-123 me
jiracli edit field    PROJ-123 "priority=High" "labels+=regression"
jiracli add comment   PROJ-123 "Reproduced on v10.5, not v10.6."
jiracli add link      PROJ-123 PROJ-456 --type Blocks

# Apply
jiracli edit status PROJ-123 "In Review" --yes
```

Every dry-run shows:
```
DRY RUN — no changes made.

POST https://jira.example.com/rest/api/2/issue/PROJ-123/transitions

Body:
  { "transition": { "id": "31" } }

Effect:
  PROJ-123: In Progress → In Review

Validation:
  ✓ transition 31 "In Review" is available on PROJ-123

Apply with: re-run with --yes
```

### Creating issues

```sh
# Inline
jiracli create --project PROJ --type Bug --summary "Login fails on iOS" --assignee me --yes

# Draft workflow (recommended for complex issues)
jiracli create --init-draft new-issue.yaml   # writes a YAML template
$EDITOR new-issue.yaml
jiracli create --from-draft new-issue.yaml   # preview with validation
jiracli create --from-draft new-issue.yaml --yes
```

### Deleting

```sh
jiracli delete comment  PROJ-123:comment:9421 --yes
jiracli delete attachment PROJ-123:attach:10042 --yes
jiracli delete link     10234 --yes
jiracli delete issue    PROJ-123 --yes
```

### Opening in browser

```sh
jiracli open PROJ-123
jiracli open PROJ-123 --print-url    # just the URL, don't open
```

---

## Output modes

| Mode | When | Format |
|---|---|---|
| Plain text | default | Human-readable, `[exit:0 \| Xms]` footer |
| NDJSON | `--json` | One JSON object per line, no footer, stable v1 schema |

Large outputs (>200 lines or >50 KB) are automatically written to `<tmpdir>/jiracli-output/output-N.txt` with exploration hints inline (`<tmpdir>` is the OS temp dir — `/tmp` on Linux, `$TMPDIR` on macOS; the hint always shows the real resolved path). `--json` is never truncated.

NDJSON schema reference: [`docs/json-schema.md`](docs/json-schema.md)

---

## Lookup

Before writing, use `lookup` to find valid names and IDs:

```sh
jiracli lookup users      "alex"  --project PROJ    # who can be assigned
jiracli lookup components         --project PROJ    # valid component names
jiracli lookup versions           --project PROJ --unreleased
jiracli lookup priorities         --project PROJ
jiracli lookup link-types                           # Blocks, Duplicates, etc.
jiracli lookup statuses   --query "In"
jiracli lookup issue-types        --project PROJ
jiracli lookup fields     "story"                   # find custom field IDs
jiracli lookup projects   "PROJ"
jiracli lookup labels     "regr"  --project PROJ
```

Metadata is cached at `~/.cache/jiracli/<profile>/` (5 min for labels, 1 h for components/versions, 24 h for everything else). Invalidate with `jiracli cache clear` or bypass with `--no-cache`.

---

## Multiple profiles

```sh
jiracli auth login --profile staging --url https://jira-staging.example.com
jiracli show PROJ-123 --profile staging
jiracli auth profile staging          # set as default
jiracli auth profile --list
```

## Docs

| Reference | Location |
|---|---|
| Setup & auth | [`docs/setup-auth.md`](docs/setup-auth.md) |
| Read commands (`show`, `search`) | [`docs/reads.md`](docs/reads.md) |
| Write commands (`edit`, `add`, `create`) | [`docs/writes.md`](docs/writes.md) |
| Delete commands | [`docs/delete.md`](docs/delete.md) |
| Lookup commands | [`docs/lookup.md`](docs/lookup.md) |
| Cache commands | [`docs/cache.md`](docs/cache.md) |
| NDJSON schemas (v1) | [`docs/json-schema.md`](docs/json-schema.md) |
