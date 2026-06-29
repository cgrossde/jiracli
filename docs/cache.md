# jiracli — Cache Commands

Reference for: `cache list`, `cache clear`.

Metadata fetched by `lookup` commands is cached at `~/.cache/jiracli/<profile-hash>/`. The hash is derived from `(profile-name, URL)` — changing the URL invalidates all entries automatically.

See [lookup.md](lookup.md) for cache TTLs per resource type.

---

## `cache list`

Lists all cached metadata entries for the active profile.

    jiracli cache list [--profile <name>] [--json]

### Flags

| Flag | Description |
|---|---|
| `--profile <name>` | Credential profile |
| `--json` | NDJSON output |

### Plain-text output shape

```
fields             saved 2h ago    ttl 24h0m0s    expires in 22h0m0s
projects           saved 5m ago    ttl 24h0m0s    expires in 23h55m0s
project/WEB        saved 30m ago   ttl 1h0m0s     expires in 30m0s
labels/WEB         saved 1m ago    ttl 5m0s       expires in 4m0s
```

### NDJSON output (`--json`)

```ndjson
{"key":"fields","savedAt":"2026-06-24T08:00:00Z","ttl":"24h0m0s","expiresAt":"2026-06-25T08:00:00Z","expired":false}
```

---

## `cache clear`

Deletes cached entries for the active profile.

    jiracli cache clear [--key <glob>] [--yes] [--profile <name>]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--key <glob>` | — | Glob pattern of keys to delete (e.g. `project/*`, `fields`, `*`) |
| `--yes` | false | Skip confirmation prompt |
| `--profile <name>` | default | Credential profile |

Without `--key`: clears **all** cached entries for the profile. With `--key`: deletes only matching keys using `filepath.Match` semantics.

In an interactive TTY (without `--yes`): prompts `Delete N cached entries? [y/N]`. Non-TTY without `--yes`: degrades gracefully (prints what would be deleted, exits 0 without deleting — add `--yes` to apply). In non-interactive CI use `--yes` explicitly.

### Auto-invalidation

A validation failure during a write command (e.g. component not found in `field set`) automatically refetches the affected cache entry (`project/<KEY>`) and retries the validation once before surfacing an error. This handles the case where a component was added by an admin between cache loads.

### Examples

```
jiracli cache clear --key 'project/WEB' --yes    # clear one project
jiracli cache clear --key 'project/*' --yes      # clear all projects
jiracli cache clear --yes                         # clear everything
```
