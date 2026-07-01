# jiracli — Documentation

Full reference for all commands, flags, output formats, and schemas.

**New here?** Start with [setup-auth.md](setup-auth.md) to authenticate, then try `jiracli show assigned` and `jiracli search "project = PROJ"`.

---

## Contents

| File | What's in it |
|---|---|
| [setup-auth.md](setup-auth.md) | First-time setup, PAT auth, profiles, skill install |
| [reads.md](reads.md) | `show`, `search`, `assigned`, `comments`, `history`, `transitions`, `attachments`, `open` |
| [writes.md](writes.md) | `add comment`, `edit status/assignee/field`, `create`, `add link`, `add attachment` — and the write safety model |
| [boards-sprints.md](boards-sprints.md) | `board`, `sprint`, `lookup boards`, `edit sprint`, `config agile` — Agile board and sprint commands |
| [delete.md](delete.md) | `delete comment`, `delete attachment`, `delete link`, `delete issue` |
| [lookup.md](lookup.md) | `lookup users/labels/components/versions/projects/issue-types/link-types/statuses/priorities/fields` |
| [cache.md](cache.md) | `cache list`, `cache clear`, auto-invalidation |
| [json-schema.md](json-schema.md) | Stable NDJSON v1 schemas for all `--json` outputs |

---

## Quick reference

### Reading

```sh
jiracli show PROJ-123                     # full issue
jiracli show PROJ-123 --no-history        # skip changelog
jiracli show comments PROJ-123            # comment thread
jiracli show history  PROJ-123 --since 7d
jiracli show assigned                     # your open issues
jiracli search "project = PROJ AND priority = High"
```

### Writing (dry-run by default, `--yes` to apply)

```sh
jiracli edit status   PROJ-123 "In Review"
jiracli edit assignee PROJ-123 me
jiracli edit field    PROJ-123 "priority=High" "labels+=regression"
jiracli add comment   PROJ-123 "Looks good."
jiracli create --project PROJ --type Bug --summary "..." --assignee me
```

### Lookup (find valid names before writing)

```sh
jiracli lookup components  --project PROJ
jiracli lookup priorities  --project PROJ
jiracli lookup link-types
jiracli lookup users "alex" --project PROJ
```

### Agile (boards and sprints)

```sh
jiracli lookup boards --project PROJ
jiracli board show 101
jiracli sprint current --board 101
jiracli sprint list --board 101            # active + future + recently-closed
jiracli sprint list --board 101 --all      # every sprint (full history)
jiracli sprint issues 2001
jiracli edit sprint ACME-123 current --board 101
jiracli edit sprint ACME-123 backlog --yes
jiracli config agile
```

---

## Output contract

All commands write to stdout with a trailing `[exit:0 | Xms]` footer. Pass `--json` for stable NDJSON output (footer suppressed). Outputs over ~200 lines or ~50 KB are truncated to `<tmpdir>/jiracli-output/output-N.txt` (`<tmpdir>` = OS temp dir: `/tmp` on Linux, `$TMPDIR` on macOS) with exploration hints showing the real path.
