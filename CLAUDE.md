Before planning any feature, read `ARCHITECTURE.md`.
Before writing any code, read `CODING-INSTRUCTIONS.md`.

# jiracli — Agent Coding Instructions

## Project purpose

`jiracli` gives an AI agent programmatic access to a Jira Data Center instance.
See `ARCHITECTURE.md` for the full design rationale.

---

## Commands

| Command | Flags | Description |
|---|---|---|
| `setup` | `--profile`, `--url`, `--pat-file`, `--no-skill`, `--reconfigure`, `--no-browser`, `--install-skill`, `--uninstall-skill` | Interactive auth + skill wizard (or skill-only with flags) |
| `auth login` | `--profile`, `--url`, `--pat-file`, `--insecure` | Save PAT credentials |
| `auth reauth` | `--profile` | Re-prompt PAT for existing profile |
| `auth logout` | `--profile`, `--all` | Remove Keychain entry |
| `auth profile` | `[profile]`, `--clear`, `--list` | Get or set default profile |
| `auth status` | `--profile`, `--json`, `--no-cache` | Show current authenticated user and credential status |
| `config hierarchy` | `--profile`, `--json`, `--portfolio`, `--rediscover` | View or update hierarchy field IDs for the profile |
| `config agile` | `--profile`, `--json`, `--rediscover`, `--field` | View or update Sprint custom-field ID for the profile |
| `show <ref> [ref...]` | `--profile`, `--json`, `--no-history`, `--no-comments`, `--fields`, `--fields-only`, `--no-children`, `--parent`, `-o` | Fetch and render one or more issues (inlines the single latest comment — use `show comments <KEY>` for the full thread); pass `-` to read keys from stdin. Compound refs (`KEY:attach:ID`) single only. |
| `show assigned` | `--profile`, `--json`, `--keys-only`, `--state`, `--limit`, `--page` | Issues assigned to the current user |
| `show comments <KEY>` | `--profile`, `--json`, `--since`, `--limit`, `--page` | Issue comment thread |
| `show history <KEY>` | `--profile`, `--json`, `--include-rank`, `--since`, `--limit`, `--page` | Changelog entries |
| `show transitions <KEY>` | `--profile`, `--json` | Available workflow transitions |
| `hierarchy <KEY>` | `--profile`, `--json`, `--all`, `--open`, `--exclude-done`, `--state`, `--depth N`, `--flat`, `--since` | Top-level. Maps an issue both ways: **up** the parent chain (Story → Epic → Initiative/portfolio) to locate it, and **down** through descendants (use `--depth`) to list everything nested under an Initiative/Epic. Filter with `--exclude-done`/`--open`/`--state todo\|in-progress\|done\|all` (shared vocabulary with `search` and `effort`). |
| `show attachments <KEY>` | `--profile`, `--json` | List attachments |
|`effort <KEY>`|`--profile`, `--json`, `--all`, `--limit N`, `--depth N`, `--exclude-done`, `--open`, `--state`, `--since`, `--group-by status\|statusCategory`|Top-level (formerly `show rollup`). Hierarchy rollup: walks the issue's children and aggregates time + story points. `--group-by status\|statusCategory` replaces per-level rows with a per-status breakdown. Reports totals only — use `jiracli hierarchy <KEY>` for a per-child breakdown. Filter with `--exclude-done`/`--open`/`--state`/`--since`. `--limit N` caps children per level (default 100); `--all` fetches everything.|
|`effort jql <query>`|`--profile`, `--json`, `--all`, `--limit N`, `--exclude-done`, `--open`, `--state`, `--since`, `--group-by assignee\|status\|statusCategory`|Aggregate time + story points over a JQL result set (no hierarchy config needed). Query joined from positional args. `--group-by assignee\|status\|statusCategory` breaks totals down by dimension.|
|`effort sprint <id>`|`--profile`, `--json`, `--all`, `--limit N`, `--exclude-done`, `--open`, `--state`, `--since`, `--group-by assignee\|status\|statusCategory`|Aggregate time + story points over a sprint's issues (no hierarchy config needed). `<id>` is the numeric sprint id.|
| `search [<jql...>]` | `--profile`, `--json`, `--keys-only`, `--exclude-done`, `--open`, `--limit`, `--page`, `--fields`, `--fields-only`, `--assigned`, `--state`, `--jql`, `--time`, `--count-by` | Search issues (`--assigned` also excludes Done unless `--state` is set; `--state all` includes Done); `--open` is an alias for `--exclude-done`; `--time` adds Estimate/Remaining/Spent columns; `--count-by FIELD` replaces the issue list with a count/percent histogram (supported fields: status, statusCategory, priority, assignee, issueType, resolution, project); paginates internally to exhaustion — `--limit` and `--page` are ignored when `--count-by` is set. |
| `open <ref>` | `--profile`, `--print-url` | Open issue/comment/attachment in browser |
| `lookup users` | `--profile`, `--project`, `--active`, `--limit`, `--json` | Search users |
| `lookup labels` | `--profile`, `--project`, `--json` | Suggest labels |
| `lookup components` | `--profile`, `--project`, `--json` | List project components |
| `lookup versions` | `--profile`, `--project`, `--released`, `--unreleased`, `--archived`, `--json` | List versions |
| `lookup projects` | `--profile`, `--json` | List projects |
| `lookup issue-types` | `--profile`, `--project`, `--json` | List issue types |
| `lookup link-types` | `--profile`, `--json` | List link types |
| `lookup statuses` | `--profile`, `--query`, `--json` | List statuses |
| `lookup priorities` | `--profile`, `--project`, `--json` | List priorities |
| `lookup fields` | `--profile`, `--custom`, `--id`, `--project`, `--type`, `--json` | List/inspect fields |
| `lookup boards` | `--profile`, `--project` (required), `--type` (scrum\|kanban), `--limit`, `--page`, `--json`, `--no-cache` | List Agile boards for a project |
| `cache list` | `--profile`, `--json` | Show cached entries with TTLs |
| `cache clear` | `--profile`, `--key`, `--yes` | Purge cache entries (use `cache list` to see key names) |
| `edit status <KEY> [KEY...] <name-or-id>` | `--profile`, `--comment`, `--yes` | Transition one or more issues; last arg is always the transition name/id |
| `edit assignee <KEY> [KEY...] <user-or-id>` | `--profile`, `--yes` | Assign one or more issues; last arg is always the user (`-` to unassign, `me` for self) |
| `edit field <KEY> [KEY...] <spec...>` | `--profile`, `--allow-new`, `--yes` | Update arbitrary fields on one or more issues; leading args without `=` are keys, first arg with `=` starts specs |
| `edit sprint <KEY> [KEY...] <target>` | `--profile`, `--board`, `--yes` | Move issues into a sprint or backlog (dry-run by default; target: numeric id, `current`, `next`, `backlog`) |
| `board list` | `--profile`, `--project` (required), `--type` (scrum\|kanban), `--limit`, `--page`, `--json`, `--no-cache` | List Agile boards for a project (alias of `lookup boards`) |
| `board show <id>` | `--profile`, `--project`, `--json`, `--no-cache` | Show board configuration (columns, type) |
| `board issues <id>` | `--profile`, `--limit`, `--page`, `--json`, `--keys-only` | List issues on a board via Agile API |
| `sprint list` | `--profile`, `--board` (required), `--state`, `--limit`, `--page`, `--json`, `--name-contains`, `--after`, `--before`, `--sort` | List sprints. Filter flags are client-side; `--state closed` defaults to newest-first (`--sort desc`). |
| `sprint show <id>` | `--profile`, `--json` | Show sprint details |
| `sprint issues <id>` | `--profile`, `--limit`, `--page`, `--json`, `--keys-only` | List issues in a sprint |
| `sprint current` | `--profile`, `--board` (required), `--assigned`, `--exclude-done`, `--json` | Show active sprint and embedded issue list for a board |
| `add comment <KEY> <body...>` | `--profile`, `--file`, `--yes` | Add comment (dry-run by default) |
| `add link <source> <target>` | `--profile`, `--type`, `--yes` | Link two issues |
| `add attachment <KEY> <file...>` | `--profile`, `--yes` | Upload attachments |
| `delete <ref> [ref...]` | `--profile`, `--yes`, `--with-subtasks` | Delete issues (multi-key OK for plain keys), or single comment/attach/link by compound ref. Aliased as `rm`. Dry-run by default. |
| `create` | `--profile`, `--init-draft`, `--from-draft`, `--project`, `--type`, `--summary`, `--description`, `--priority`, `--assignee`, `--component`, `--label`, `--fix-version`, `--custom`, `--allow-new`, `--no-cache`, `--yes` | Create issue |

Full usage details: `docs/` directory.

---

## Output contract

### Default (plain text)

Every command writes to stdout with a trailing footer:

```
[output]
[exit:0 | 12ms]
```

On failure, stderr is always included:

```
[stdout if any]
[stderr] reason here
[exit:1 | 3ms]
```

### Overflow (progressive disclosure)

Output exceeding **~200 lines or ~50 KB** is automatically truncated. Full
content at `/tmp/jiracli-output/output-N.txt`.

### JSON mode (`--json`)

Commands with structured output support `--json`. Output is NDJSON — one JSON
object per line. The presenter is bypassed entirely. Errors go to stderr only.
`--json` field names and types are a stability contract.

**Pagination:** final line of stdout is `{"_pagination": {...}}` when more
results exist. No trailer on last page.

---

## Architecture

Two-layer model — see `ARCHITECTURE.md` for the full design rationale.

- **Layer 1** (`cmd/`, `internal/`): executes, returns `(string, error)`, no truncation or annotation
- **Layer 2** (`internal/output/`, wired in `main.go`): overflow, footer, stderr attachment

---

## Reference

- `ARCHITECTURE.md` — two-layer model, output modes, design constraints
- `CODING-INSTRUCTIONS.md` — Go style, error handling, testing rules
- `docs/` — per-command reference docs
