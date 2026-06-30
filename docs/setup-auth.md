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
| `--no-skill` | false | Skip Step 5 (skill install) |
| `--reconfigure` | false | Force all prompts even when state is valid |
| `--no-browser` | false | Print PAT-creation URL but never call `open` |

### Flow

Five steps, each independently idempotent:

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

**Step 3 — Hierarchy field discovery**
- Auto-resolves the Epic Link and Parent Link custom-field IDs by querying `GET /rest/api/2/field` and matching by name.
- Lists portfolio-level field candidates (fields whose name contains 'initiative', 'program', 'feature', 'theme', or 'portfolio') and prompts the user to pick one.
- Stores the result in `HierarchyConfig` inside the Keychain entry.
- Short-circuit: if `EpicLinkField` is already set in the stored entry and `--reconfigure` is not given, prints `✓ Hierarchy already configured (Epic=... Parent=... Portfolio=...)` and skips.
- Fields not found (e.g. Jira Software not installed) print an advisory and continue — the step is non-fatal.

**Step 4 — Agile / Sprint field discovery**
- Calls `GET /rest/api/2/field` and finds the field where `Name == "Sprint"` and schema custom type is `com.pyxis.greenhopper.jira:gh-sprint`. Falls back to `Name == "Sprint"` with array type.
- Stores the result in `AgileConfig.SprintField` inside the Keychain entry.
- Short-circuit: if `SprintField` is already set and `--reconfigure` is not given, prints `✓ Sprint field already configured (customfield_XXXXX)` and skips.
- Not found (Jira Software not installed): prints advisory and continues — non-fatal.

**Step 5 — Skill install**
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
  "insecure": false,
  "hierarchy": {
    "epicLinkField": "customfield_10100",
    "parentLinkField": "customfield_10101",
    "portfolioField": "customfield_10102",
    "portfolioFieldName": "Initiative Link",
    "discoveredAt": "2026-06-29T10:00:00Z"
  },
  "agile": {
    "sprintField": "customfield_10050",
    "discoveredAt": "2026-06-29T10:00:00Z"
  }
}
```

`hierarchy` is `omitempty` — omitted on legacy profiles that haven't run setup step 3.

`agile` is `omitempty` — omitted on profiles that haven't discovered the sprint field.

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


---

## `config hierarchy`

View or update the hierarchy field IDs stored for a credential profile. These IDs are used by `show <KEY>` (to populate `epic` and `portfolio`) and `show hierarchy <KEY>` (to walk the ancestor chain).

    jiracli config hierarchy [flags]

### Flags

| Flag | Default | Description |
|---|---|---|
| `--profile <name>` | default profile | Credential profile |
| `--json` | false | NDJSON output |
| `--rediscover` | false | Re-run field discovery: resolves Epic Link + Parent Link by name, scans portfolio candidates, prompts interactively |
| `--portfolio <id\|name\|none>` | — | Set or clear the Portfolio field directly (bypasses interactive scan) |

### Output (plain text, no flags)

    Hierarchy config for profile "default":
      Epic Link        : customfield_10100
      Parent Link      : customfield_10101
      Portfolio        : customfield_10102
      Portfolio (name) : Initiative Link
      Discovered at    : 2026-06-29T10:00:00Z

When a field is not configured, `—` is shown.

### Output (`--json`)

```json
{"epicLinkField":"customfield_10100","parentLinkField":"customfield_10101","portfolioField":"customfield_10102","portfolioFieldName":"Initiative Link","discoveredAt":"2026-06-29T10:00:00Z"}
```

### Usage patterns

```bash
# View current config
jiracli config hierarchy

# Re-discover all fields interactively (after setup or field ID changes)
jiracli config hierarchy --rediscover

# Set portfolio field by name
jiracli config hierarchy --portfolio "Initiative Link"

# Set portfolio field by custom field ID
jiracli config hierarchy --portfolio customfield_10102

# Clear portfolio field
jiracli config hierarchy --portfolio none
```

### Errors

- No credentials: `[stderr] no credentials found — run: jiracli setup`, exit 1.
- Unknown field: `[stderr] unknown field "customfield_99999" — run: jiracli lookup fields`, exit 1.

---

## `config agile`

View or update the Sprint custom-field ID for a credential profile. Used by `show <KEY>` to render the Sprint section and by `search --fields sprint`.

    jiracli config agile [flags]

### Flags

|Flag|Default|Description|
|---|---|---|
|`--profile <name>`|default profile|Credential profile|
|`--json`|false|NDJSON output|
|`--rediscover`|false|Re-run sprint field discovery against the live `/field` list|
|`--field <id\|name\|none>`|—|Set the sprint field explicitly; `none` clears it|

### Output (plain text, no flags)

    Agile config for profile "default":
      Sprint field : customfield_10050
      Discovered at: 2026-06-29T10:00:00Z

A live probe line follows showing whether the resolved field matches the stored value.

### Output (`--json`)

```json
{"sprintField":"customfield_10050","discoveredAt":"2026-06-29T10:00:00Z"}
```

### Usage patterns

```bash
# View current sprint field config
jiracli config agile

# Re-discover after installing Jira Software
jiracli config agile --rediscover

# Set explicitly by field id
jiracli config agile --field customfield_10050

# Clear
jiracli config agile --field none
```

### Errors

- No credentials: `[stderr] no credentials found — run: jiracli setup`, exit 1.
- Unknown field: `[stderr] unknown field "X" — run: jiracli lookup fields`, exit 1.
