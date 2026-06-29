# jiracli — Setup and Auth

Reference for: `setup`, `auth login/reauth/status/logout/profile`, `skill install/uninstall`, `auth status`.

---

## `setup`

Interactive first-time wizard. Idempotent — each step short-circuits when state is already valid.

    jiracli setup [flags]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--profile <name>` | `default` | Target credential profile |
| `--url <url>` | — | Pre-fill Step 1 (skips URL prompt) |
| `--pat-file <path>` | — | Read PAT from file (CI/scripted use) |
| `--no-skill` | false | Skip Step 3 (skill install) |
| `--reconfigure` | false | Force all prompts even when state is valid |
| `--no-browser` | false | Print PAT-creation URL but never call `open` |

### Flow

Three steps, each independently idempotent:

**Step 1 — Jira server URL**
- Prompts for base URL; `--url` pre-fills.
- Probes `GET /rest/api/2/serverInfo` (anonymous). On success prints `ok (Jira <deploymentType> <version>)`.
- TLS error → prompts `Continue with --insecure? [y/N]`. Yes sets `insecure: true` in the Keychain blob.
- Short-circuit: existing profile with valid URL → `Step 1: ✓ Server already configured (<url>)` (unless `--reconfigure`).

**Step 2 — Personal Access Token**
- Constructs deep link to the instance's PAT management page and prints it.
- Prompts `Open in browser now? [Y/n]` (default Y). On Y calls `open` (macOS) / `xdg-open` (Linux).
- PAT source: `--pat-file` → hidden terminal prompt. Input is never echoed.
- Verifies via `GET /rest/api/2/myself`. On 401: "Token rejected — paste again, or hit ENTER to abort." Up to 3 retries.
- On success: saves `{url, pat, user, displayName, savedAt, insecure?}` to Keychain service `jiracli` under the profile name. Sets that profile as the default if no default exists yet.
- Short-circuit: stored PAT passes `/myself` probe → `Step 2: ✓ PAT still valid (<displayName>)`. On 401, drops into prompt loop regardless.

**Step 3 — Skill install**
- Byte-compares the embedded `SKILL.md` against `~/.claude/skills/jira/SKILL.md`.
- Missing → prompts `Install? [Y/n]` (default Y).
- Outdated → prompts `Skill exists but is outdated. Update? [Y/n]`.
- Up to date → `✓ Skill already installed` (no prompt).
- Skip with `--no-skill`.

Closing banner: `Done! Try: jiracli auth status   or   jiracli show assigned`.

### Keychain blob

Service `jiracli`, account = profile name. Stored as JSON:

```json
{
  "profile": "default",
  "url": "https://jira.example.com",
  "kind": "dc-pat",
  "pat": "<token>",
  "user": "u1",
  "displayName": "Alex Chen",
  "savedAt": "2026-06-24T10:00:00Z",
  "insecure": false
}
```

`kind` is always `"dc-pat"` in v1. `insecure` is omitted when false.

### Errors

- `[stderr] PAT in keychain for profile "default" was rejected (HTTP 401) — run: jiracli auth reauth`
- `[stderr] no credentials found — run: jiracli setup`

---

## `auth login`

Scriptable equivalent of `setup`: saves credentials without wizard prompts, browser, or skill install.

    jiracli auth login --url <url> [flags]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--url <url>` | — | Jira server base URL (**required**) |
| `--profile <name>` | `default` | Credential profile name |
| `--pat-file <path>` | — | Read PAT from file |
| `--insecure` | false | Skip TLS certificate verification |

PAT source order: `--pat-file` → `JIRACLI_PAT` env var → hidden terminal prompt.

### Output (plain text)

Single line on stdout:

    ✓ saved profile "default" (user u1)

Followed by `[exit:0 | Xms]` footer.

### Errors

- `--url` absent → `[stderr] --url is required — run: jiracli auth login --url <jira-url>`
- PAT verification fails → `[stderr] PAT verification failed: PAT in keychain for profile "default" was rejected (HTTP 401) — run: jiracli auth reauth`

---

## `auth reauth`

Deletes the existing Keychain entry, then re-runs the login flow (preserving the saved URL).

    jiracli auth reauth [--profile <name>] [--url <url>]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--profile <name>` | default profile | Profile to re-authenticate |
| `--url <url>` | — | Override Jira server URL (required when no prior entry exists) |

Re-prompts for the PAT. URL is read from the prior entry; if no prior entry exists, `--url` is required. Accepts `--pat-file` and `JIRACLI_PAT` env as sources.

### Errors

- No prior entry and no `--url` → `[stderr] credentials not found for profile "X" — run: jiracli setup`

---

## `auth logout`

Removes the Keychain entry for a profile.

    jiracli auth logout [--profile <name>] [--all]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--profile <name>` | default profile | Profile to remove |
| `--all` | false | Remove every profile and clear the default |

In an interactive TTY (without `--yes`), prompts `Remove profile "X"? [y/N]`. In non-TTY, deletes without prompting. If the deleted profile was the default, `DeleteDefault()` is called automatically.

---

## `auth profile`

Gets or sets the default credential profile.

    jiracli auth profile [name]
    jiracli auth profile --list
    jiracli auth profile --clear

### Flags

| Flag | Description |
|---|---|
| `--list` | List all profiles; default marked with `*` |
| `--clear` | Delete the default pointer (profiles remain) |

- No argument: prints current default name, or `none`.
- One argument: sets that profile as the new default → `✓ default profile set to "X"`.

---

## `skill install`

Installs or refreshes `~/.claude/skills/jira/SKILL.md` from the binary's embedded copy.

    jiracli setup --install-skill [--profile <name>]

Byte-compares embedded vs installed:
- File missing → writes it → `✓ skill installed at <path>`
- Files differ → overwrites → `✓ skill updated at <path>`
- Files identical → `✓ skill already up to date at <path>`

Creates parent directory `~/.claude/skills/jira/` (mode 0755) if absent. Writes file at mode 0644.

---

## `skill uninstall`

Removes `~/.claude/skills/jira/SKILL.md`. Does not remove the parent directory.

    jiracli setup --uninstall-skill [--profile <name>]

If the file is absent: `skill not installed (no file at <path>)`, exit 0. No error either way.

---

## `auth status`

Prints the authenticated user and credential status for the active profile. This is the primary way to verify credentials are working.

    jiracli auth status [--profile <name>] [--json] [--no-cache]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--profile <name>` | default profile | Credential profile |
| `--json` | false | NDJSON output |
| `--no-cache` | false | Bypass the 24h `myself` cache and fetch live data |

### Output (plain text)

    profile: default
    url:     https://jira.example.com
    user:    Jane Smith (alice)
    email:   alice@example.com
    saved:   2026-06-24T10:00:00Z
    status:  ✓ authenticated

### Output (`--json`)

```json
{"profile":"default","url":"https://jira.example.com","name":"alice","displayName":"Jane Smith","emailAddress":"alice@example.com","savedAt":"2026-06-24T10:00:00Z","active":true,"authenticated":true,"error":null}
```

`error` is a string on failure, `null` on success. On authentication failure: `authenticated: false`, exit 1.

### Errors

- No entry → `[stderr] no credentials found for profile "X" — run: jiracli setup`, exit 1.
- 401 → `[stderr] PAT in keychain for profile "X" was rejected (HTTP 401) — run: jiracli auth reauth`, exit 1.

