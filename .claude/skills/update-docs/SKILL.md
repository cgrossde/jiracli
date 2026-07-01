---
name: update-docs
description: Update docs/, CLAUDE.md, CODING-INSTRUCTIONS.md, and ARCHITECTURE.md to match the current source code. Use when the user asks to update docs, sync documentation, or invokes /update-docs.
allowed-tools:
  - Bash
  - Read
  - Edit
  - Write
  - Search
  - Task
  - Ask
---

# Update jiracli Docs

Keep all documentation in sync with the current source. The targets are:

| File | Covers |
|------|--------|
| `CLAUDE.md` | Command table, flags, output contract, architecture summary — agent-facing quick reference |
| `CODING-INSTRUCTIONS.md` | Go style, error handling, testing rules — developer conventions |
| `ARCHITECTURE.md` | Two-layer model, output modes, per-command JSON schemas, design constraints |
| `docs/reads.md` | `show`, `search`, `open`, `auth status` — read-only commands: flags, output format, JSON schema |
| `docs/writes.md` | `create`, `edit`, `add` — mutation commands: flags, dry-run behaviour, JSON schema |
| `docs/lookup-cache.md` | `lookup` and `cache` subcommands: flags, output format, JSON schema |
| `docs/setup-auth.md` | `setup`, `auth login/reauth/logout/profile` — credential management |
| `docs/json-schema.md` | Cross-command NDJSON field reference and pagination contract |

---

## Privacy rule — MUST apply everywhere

All documentation — examples, output samples, JSON snippets, flag descriptions, rendered output blocks — MUST use generic fictional identifiers only. NEVER use real names, real usernames, real email addresses, real project keys, real issue keys, or any other identifier that could identify a specific person or organisation.

Use instead:
- Issue keys: `ACME-123`, `PROJ-456`, `WEB-789`
- Users: `alice`, `bob`, or `Jane Smith` / `John Doe`
- Email: `alice@example.com`
- Project keys: `ACME`, `PROJ`, `WEB`
- URLs: `https://jira.example.com`
- Summaries: generic task descriptions ("Fix login page timeout", "Add dark mode toggle")

This applies when writing new content AND when editing existing content — replace any real identifiers encountered with the fictional equivalents above.

---

## Step 1: Identify what changed

```bash
git diff HEAD~10..HEAD --name-only
```

Adjust the depth if the branch is newer. Map changed source files to affected docs:

| Changed source | Docs to check |
|---|---|
| `cmd/show.go`, `cmd/issue.go`, `cmd/comments.go`, `cmd/history.go`, `cmd/transitions.go`, `cmd/attachments.go`, `cmd/attachment.go`, `cmd/assigned.go` | `docs/reads.md`, `CLAUDE.md` |
| `cmd/search.go` | `docs/reads.md`, `CLAUDE.md` |
| `cmd/open.go` | `docs/reads.md`, `CLAUDE.md` |
| `cmd/me.go` (auth status) | `docs/reads.md`, `docs/setup-auth.md`, `CLAUDE.md` |
| `cmd/show_hierarchy.go` | `docs/reads.md`, `docs/json-schema.md`, `CLAUDE.md` |
| `internal/jira/hierarchy.go`, `internal/jira/hierarchy_render.go` | `docs/reads.md`, `docs/json-schema.md`, `ARCHITECTURE.md` |
| `cmd/create.go`, `cmd/edit.go`, `cmd/field.go`, `cmd/assign.go`, `cmd/transition.go`, `cmd/add.go`, `cmd/comment.go`, `cmd/link.go`, `cmd/attach.go` | `docs/writes.md`, `CLAUDE.md` |
| `cmd/lookup.go`, `cmd/lookup_*.go` | `docs/lookup-cache.md`, `CLAUDE.md` |
| `cmd/cache.go` | `docs/lookup-cache.md`, `CLAUDE.md` |
| `cmd/auth.go`, `cmd/setup.go` | `docs/setup-auth.md`, `CLAUDE.md` |
| `internal/keychain/` | `docs/setup-auth.md`, `CLAUDE.md` |
| `internal/output/presenter.go` | `ARCHITECTURE.md`, `CLAUDE.md` |
| `internal/jira/issue.go`, `internal/jira/search.go` | `docs/json-schema.md`, `docs/reads.md` |
| `main.go` | `ARCHITECTURE.md`, `CLAUDE.md` |

Read only the source files relevant to changed docs. Do not read unchanged packages.

---

## Step 2: Identify gaps

Compare source to doc. Look for:

- **New commands or subcommands** not in `CLAUDE.md`'s command table
- **New flags** not listed in the relevant doc
- **Changed output format** — new fields, renamed fields, new lines
- **New JSON fields** missing from the schema table in `docs/json-schema.md`
- **Removed or renamed flags/fields**
- **New behaviour** worth documenting

Do **not** update without user approval:
- **`ARCHITECTURE.md`** structural changes — anything that modifies the two-layer model, output mode rules, or design constraint prose. Additive factual updates (new package in tree, new field in JSON schema table) are fine without asking.
- **`CODING-INSTRUCTIONS.md`** — any change, because every rule here affects all future code in the project.
- **`CLAUDE.md`** command table and output contract — additions are fine; removing or changing an existing entry requires approval.

For these, stop before editing and present the proposed change:

```
Proposed change to ARCHITECTURE.md:
  Section "Package Structure" — add cmd/assigned.go entry
  Reason: new file added in recent commits

Approve? (yes / no / edit)
```

Apply only on explicit approval.

Silently update (no approval needed):
- `docs/*.md` — command docs are always kept current
- `CLAUDE.md` — adding a new command row or new flags to an existing row
- `ARCHITECTURE.md` — additive factual edits: new file in package tree, new field/helper in JSON schema table

---

## Step 3: Fan out with subagents

Once gaps are identified, use `task` subagents to update multiple docs in parallel — one subagent per target file (or per independent section when two sections of the same file are unrelated). This is the preferred approach for 3+ changes.

Each subagent assignment MUST include:
- The exact file path and line numbers (read the file first to confirm)
- The exact content to add/change, with privacy-rule-compliant identifiers
- Source-of-truth facts (struct field names, flag strings from source code) — never ask the subagent to read source itself unless the source is small and unchanged
- Acceptance criteria (what the final state should look like)

Do NOT fan out:
- When a single file has 1–2 small surgical edits (do them yourself)
- When changes in two files must be consistent with each other (coordinate via the context or sequence them)

### Coordination guidance for subagents

If two subagents could touch the same file, DM each other via `irc` to serialise. The spawner should note which agents share files in the task context.

---

## Step 4: Update each stale doc

### `CLAUDE.md`

- **Command table**: one row per command, flags column lists all flags
- **Output contract**: reflect any new output fields or changed footer behaviour
- **Reference section**: add links to any new doc files

### `CODING-INSTRUCTIONS.md`

Update only when:
- A new project-wide pattern was established
- An existing rule was changed or a new exception added
- A new external dependency was added with rationale

### `ARCHITECTURE.md`

- **Package structure** tree: add new files, remove deleted ones
- **JSON output schema table**: add new fields for commands that gained them
- **Design constraints**: only update if a constraint changed

### `docs/reads.md`

Covers: `show <ref>`, `show assigned`, `show comments`, `show history`, `show transitions`, `show attachments`, `hierarchy`, `effort`, `search`, `open`, `auth status`.

**Flags section** — one row per flag in a Markdown table:
```
| Flag | Description |
|---|---|
| `--flag-name <val>` | Description. Default noted when non-obvious. |
```

**Output format** — show a realistic rendered example. Use fictional values (`ACME-123`, `In Progress`, `Jane Smith`), not `<placeholder>`.

**JSON schema table** — columns: `Field | Type | Notes`. Notes explain semantics, not Go types. Fields marked `omitempty` in source: "omitted when zero/empty".

### `docs/writes.md`

Covers: `create`, `edit status`, `edit assignee`, `edit field`, `add comment`, `add link`, `add attachment`.

Document the dry-run default and `--yes` behaviour for every mutating command.

### `docs/lookup-cache.md`

Covers: all `lookup` subcommands and `cache list` / `cache clear`.

### `docs/setup-auth.md`

Covers: `setup`, `auth login`, `auth reauth`, `auth logout`, `auth profile`.

### `docs/json-schema.md`

Cross-command reference. Update when:
- A `--json` output struct gains or loses a field
- A new command adds `--json` support
- The pagination trailer contract changes

**Keep examples concrete** — realistic values and identifiers, not angle-bracket placeholders.

---

## Step 5: Verify

After editing, re-read each changed doc:
- No section references a flag or field that no longer exists
- JSON schema table matches actual `json:"..."` struct tags in source
- Every accepted input form is listed
- The `## Implementation` section (if present) names the correct files
- Privacy rule satisfied: no real names, project keys, or URLs

---

## Step 6: Report

Print a one-line summary per file touched:

```
CLAUDE.md              — updated: hierarchy now lists all 8 flags
docs/reads.md          — updated: hierarchy --open/--flat/--since sections; fixed JSON block; truncation note
docs/json-schema.md    — updated: hierarchy descendantsTruncated + flat mode; search description/reporter/fixVersions
ARCHITECTURE.md        — updated: hierarchy.go and hierarchy_render.go package map entries
CODING-INSTRUCTIONS.md — no changes needed
docs/writes.md         — no changes needed
docs/lookup-cache.md   — no changes needed
docs/setup-auth.md     — no changes needed
```

Do not commit. The user decides when to commit.
