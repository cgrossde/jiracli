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
| `show <ref>` | `--profile`, `--json`, `--no-history`, `--no-comments`, `--comments N`, `--fields`, `--fields-only`, `--no-children`, `--parent`, `-o` | Fetch and render one issue, or download attachment by compound ref (`-o` only used with `KEY:attach:ID` refs) |
| `show assigned` | `--profile`, `--json`, `--category`, `--limit`, `--page` | Issues assigned to the current user |
| `show comments <KEY>` | `--profile`, `--json`, `--since`, `--limit`, `--page` | Issue comment thread |
| `show history <KEY>` | `--profile`, `--json`, `--include-rank`, `--since`, `--limit`, `--page` | Changelog entries |
| `show transitions <KEY>` | `--profile`, `--json` | Available workflow transitions |
| `show hierarchy <KEY>` | `--profile`, `--json`, `--all`, `--open`, `--status`, `--depth N`, `--flat`, `--since` | Walk Initiative → Epic → Subject → Children for an issue |
| `show attachments <KEY>` | `--profile`, `--json` | List attachments |
| `search <jql...>` | `--profile`, `--json`, `--exclude-done`, `--limit`, `--page`, `--fields`, `--fields-only`, `--assigned`, `--category` | Search issues; all issues returned by default including Done; `--exclude-done` hides Done; `--category` filters by status category (todo, in-progress, done, all); `--assigned` restricts to current user |
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
| `cache list` | `--profile`, `--json` | Show cached entries with TTLs |
| `cache clear` | `--profile`, `--key`, `--yes` | Purge cache entries (use `cache list` to see key names) |
| `edit status <KEY> <name-or-id>` | `--profile`, `--comment`, `--yes` | Transition issue status |
| `edit assignee <KEY> <user-or-id>` | `--profile`, `--yes` | Assign issue (`-` to unassign, `me` for self) |
| `edit field <KEY> <spec...>` | `--profile`, `--allow-new`, `--yes` | Update arbitrary issue fields |
| `add comment <KEY> <body...>` | `--profile`, `--file`, `--yes` | Add comment (dry-run by default) |
| `add link <source> <target>` | `--profile`, `--type`, `--yes` | Link two issues |
| `add attachment <KEY> <file...>` | `--profile`, `--yes` | Upload attachments |
| `delete <ref>` | `--profile`, `--yes`, `--with-subtasks` | Delete by ref shape: `KEY` = issue, `KEY:comment:ID` = comment, `KEY:attach:ID` = attachment, `KEY:link:ID` = link. `--with-subtasks` for issue deletes only. Aliased as `rm`. Dry-run by default. |
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
