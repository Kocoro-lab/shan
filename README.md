# Shannon CLI (`shan`)

Interactive AI agent CLI powered by Shannon. 16 local tools for computer control, MCP client for third-party integrations (GitHub, Slack, databases, etc.), and remote research/swarm orchestration via the Gateway API. macOS focused.

## Quick Start

```bash
# Build from source
go build -o shan .

# Install to GOPATH
go install -ldflags "-X github.com/Kocoro-lab/shan/cmd.Version=0.1.0" .

# Run
shan                          # interactive TUI
shan "explain main.go"        # one-shot mode
shan -y "open Chrome"         # one-shot, auto-approve all tools
shan mcp serve                # MCP server over stdio
```

Or install via script:

```bash
curl -fsSL https://raw.githubusercontent.com/Kocoro-lab/shannon-cli/main/install.sh | sh
```

## Requirements

- **Go 1.24+** for building
- **macOS** (clipboard, notifications, AppleScript, screencapture, Quartz mouse control)
- **Shannon Gateway** at configurable endpoint (for LLM completions + remote tools)
- **Python 3 + pyobjc-framework-Quartz** (for `computer` tool mouse/click control)
- **Chrome** (for `browser` tool — chromedp with isolated profile)

## Architecture

```
                         shan (Go binary)
                              |
        +----------+----------+----------+----------+
        |          |          |          |          |
  Agent Loop  MCP Server  MCP Client  Remote Orch  Local Tools
  (chat loop) (stdio out) (stdio in)  (/research)  (16 tools)
        |          |          |          |          |
  POST /v1/   tools/list  Connect to  POST /api/  file, bash,
  completions tools/call  external    tasks/stream applescript,
  (Gateway)              MCP servers  (Gateway)   browser, ...
        |          |          |          |
  LLM returns  Exposes 16  GitHub,     Returns
  text or      local tools Slack, DB,  workflow_id
  function_call to clients filesystem       |
        |                     |        SSE stream
  If tool call:         Tools merged
    - Permission check  into agent loop
    - Pre-tool hook
    - Execute locally or via MCP
    - Post-tool hook
    - Audit log
    - Send result back
    - Repeat (max 25 turns)
        |
  If text: display to user
  Save to local session
```

### Data Flow Details

- **Gateway Client** (`internal/client/gateway.go`): Sends `POST /v1/completions` with messages + tool schemas. Also handles task submission and server-side tool execution. 120s timeout.
- **Agent Loop** (`internal/agent/loop.go`): Iterates LLM calls with permission checks, hooks, and audit logging at each step. Detects infinite loops (same tool+args 3x = forced text response). Accumulates token usage across turns. 5-layer token-budgeted system prompt.
- **SSE Parser** (`internal/client/sse.go`): Spec-compliant SSE parser. Handles multi-line `data:` fields, 4MB buffer for large payloads.

### Authentication

| Service | Auth |
|---------|------|
| Gateway | API key via `X-API-Key` header |

Configured via `api_key` in `~/.shannon/config.yaml` or `shan --setup`.

## CLI Usage

```bash
shan                              # interactive TUI
shan "query"                      # one-shot mode (prompts for tool approval)
shan -y "query"                   # one-shot, auto-approve all tools
shan --setup                      # configure endpoint + API key
shan mcp serve                    # start MCP server over stdio
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--yes` | `-y` | Auto-approve all tool calls in one-shot mode |
| `--setup` | | Run interactive setup wizard |

## Commands

Type `/` in the TUI to see the interactive command menu:

| Command | Description |
|---------|-------------|
| `/help` | Show help |
| `/research [quick\|standard\|deep] <query>` | Remote research via Gateway |
| `/swarm <query>` | Multi-agent swarm orchestration |
| `/copy` | Copy last response to clipboard |
| `/model [small\|medium\|large]` | Switch model tier |
| `/config` | Show merged config with sources |
| `/sessions` | Interactive session picker |
| `/session new` | Start new session |
| `/session resume <n>` | Resume session by number or ID |
| `/clear` | Clear screen |
| `/update` | Self-update from GitHub releases |
| `/quit` | Exit |
| `/<custom>` | Custom commands from `.shannon/commands/*.md` |

## Local Tools

16 local tools executed on your macOS machine:

### File Operations

| Tool | Approval | Description |
|------|----------|-------------|
| `file_read` | No | Read files with line numbers, supports offset/limit |
| `file_write` | Yes | Write/create files, creates parent dirs |
| `file_edit` | Yes | Find-and-replace (old_string must be unique) |
| `glob` | No | Find files by pattern (supports `**` recursive) |
| `grep` | No | Search file contents (ripgrep, falls back to grep) |
| `directory_list` | No | List directory contents with sizes |

### System & Shell

| Tool | Approval | Description |
|------|----------|-------------|
| `bash` | Auto for safe | Shell commands, 120s timeout, safe commands auto-approved |
| `system_info` | No | OS, arch, hostname, CPU, memory, disk |
| `process` | Auto for list/ports | Process management: list, ports, kill |
| `http` | Network allowlist | HTTP client, localhost auto-approved |

### macOS Control

| Tool | Approval | Description |
|------|----------|-------------|
| `clipboard` | Yes | Read/write system clipboard (pbcopy/pbpaste) |
| `notify` | Yes | macOS desktop notifications via osascript |
| `applescript` | Yes | Execute arbitrary AppleScript |
| `screenshot` | Yes | Screen capture (fullscreen/window/region) |
| `computer` | Yes | OS-level mouse/keyboard (click/type/hotkey/move) |
| `browser` | Yes | Chromedp with isolated Chrome profile (navigate/click/type/screenshot/read_page/execute_js/wait/close) |

### Tool Approval Flow

```
Tool call from LLM
  → Permission engine (hard-block → denied_commands → allowed_commands)
  → RequiresApproval + SafeChecker
  → Pre-tool hook (can deny)
  → Execute tool
  → Post-tool hook
  → Audit log
```

- **Hard-blocked**: `rm -rf /`, `mkfs`, `dd if=`, `curl|sh`, etc. — always denied, cannot be overridden
- **Auto-approve**: Safe bash commands (`ls`, `git status`, `go test`, `make`, etc.), `process list/ports`, localhost HTTP
- **Prompt**: Destructive tools show `[y/n]` in TUI or one-shot mode
- **`-y` flag**: Auto-approves everything in one-shot mode
- **No handler**: Denied by default (security fail-safe)

## Permission Engine

5-layer command checking:

1. **Hard-block** — built-in constants (rm -rf /, mkfs, dd, curl|sh, etc.), cannot be overridden
2. **Denied commands** — `permissions.denied_commands` in config
3. **Shell AST parsing** — compound commands split on `&&`, `||`, `;`, `|`, each sub-command checked independently
4. **Allowed commands** — `permissions.allowed_commands` in config (glob patterns)
5. **User approval** — interactive prompt or `-y` flag

Additional checks:
- **File paths**: symlink protection (`filepath.EvalSymlinks`), sensitive file patterns (`.env`, `*.pem`, `id_rsa`), allowed_dirs
- **Network egress**: allowlist-based, localhost always allowed
- **Hooks**: PreToolUse can deny with exit code 2

## Audit Logging

All tool calls are logged to `~/.shannon/logs/audit.log`:

- JSON-lines format, append-only
- Each entry: timestamp, session ID, tool name, input/output summary, decision, approved, duration
- **Auto-redaction**: AWS keys, JWT, `sk-`/`key-` prefixes, Bearer tokens, PEM markers, env var assignments

## Hooks

Shell scripts triggered at lifecycle events:

| Hook | When | Can Deny |
|------|------|----------|
| `PreToolUse` | Before tool execution | Yes (exit 2) |
| `PostToolUse` | After tool execution | No |
| `SessionStart` | Session begins | No |
| `Stop` | Session ends | No |

Configure in `~/.shannon/config.yaml`:

```yaml
hooks:
  PostToolUse:
    - matcher: "file_edit|file_write"
      command: ".shannon/hooks/post-edit.sh"
```

Hook protocol:
- Receives JSON on stdin with tool name, arguments, result
- Exit 0 = allow, exit 2 = deny (PreToolUse only)
- 10s timeout, 10KB output limit

## MCP Server

Expose all 16 local tools to MCP clients via JSON-RPC 2.0 over stdio:

```bash
shan mcp serve
```

Supported methods:
- `initialize` — handshake with protocol version
- `tools/list` — returns all 16 tool schemas
- `tools/call` — execute a tool by name with arguments

Example:
```bash
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' | shan mcp serve
echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"system_info","arguments":{}}}' | shan mcp serve
```

## MCP Client

Connect to external MCP servers to extend Shannon with third-party tools. Configure in `~/.shannon/config.yaml` under `mcp_servers:`.

Each server supports:
- `command` / `args` — stdio transport (default)
- `type: http` + `url` — HTTP transport
- `env` — environment variables passed to the server process
- `context` — guidance injected into the LLM system prompt (critical for correct tool usage)
- `disabled: true` — skip without removing config

### GitHub

```yaml
mcp_servers:
  github:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "ghp_xxxxx"
    context: "Authenticated as GitHub user 'yourname'. Use search_repositories with query 'user:yourname'."
```

```bash
shan "list my github repos"
shan "create an issue in myrepo titled 'Bug: login fails'"
shan "show open PRs in shannon-cli"
```

### Slack

```yaml
mcp_servers:
  slack:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-slack"]
    env:
      SLACK_BOT_TOKEN: "xoxb-xxxxx"
      SLACK_TEAM_ID: "T01234567"
    context: "Connected to Slack workspace 'MyTeam'. Use list_channels to find channels."
```

```bash
shan "list slack channels"
shan "search slack for messages about deployment"
```

### Filesystem

```yaml
mcp_servers:
  filesystem:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/Users/you/Desktop", "/Users/you/Documents"]
    context: "Filesystem access to ~/Desktop and ~/Documents. Use read_file, write_file, list_directory, search_files."
```

```bash
shan "list files on my Desktop"
shan "search for .md files in my Documents"
shan "create a notes.txt file on my Desktop"
```

### Puppeteer (Browser Automation)

```yaml
mcp_servers:
  puppeteer:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-puppeteer"]
    context: "Browser automation via Puppeteer. Use puppeteer_navigate, puppeteer_screenshot, puppeteer_click, puppeteer_fill, puppeteer_evaluate."
```

```bash
shan -y "navigate to https://example.com and take a screenshot"
shan -y "navigate to https://news.ycombinator.com and get the top 5 story titles"
```

### PostgreSQL

```yaml
mcp_servers:
  postgres:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-postgres", "postgresql://user:pass@localhost:5432/mydb"]
    context: "Connected to mydb PostgreSQL database. Use query tool for SELECT."
```

```bash
shan "show all tables in the database"
shan "how many users signed up this week?"
```

### SQLite

```yaml
mcp_servers:
  sqlite:
    command: "npx"
    args: ["-y", "mcp-server-sqlite-npx", "/path/to/database.db"]
    context: "Connected to SQLite database. Use read_query for SELECT, write_query for writes."
```

### Brave Search

```yaml
mcp_servers:
  brave-search:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-brave-search"]
    env:
      BRAVE_API_KEY: "BSAxxxxx"
    context: "Use brave_web_search for web queries. Use brave_local_search for local businesses."
```

### Google Maps

```yaml
mcp_servers:
  google-maps:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-google-maps"]
    env:
      GOOGLE_MAPS_API_KEY: "AIzaxxxxx"
    context: "Use maps_search_places to find places. Use maps_directions for routing."
```

### Sentry (Error Tracking)

```yaml
mcp_servers:
  sentry:
    command: "npx"
    args: ["@sentry/mcp-server"]
    env:
      SENTRY_ACCESS_TOKEN: "sntrys_xxxxx"
    context: "Connected to Sentry org. Use to query issues, error events, and stack traces."
```

### Linear (Project Management)

```yaml
mcp_servers:
  linear:
    command: "npx"
    args: ["-y", "@linear/mcp-server"]
    env:
      LINEAR_API_KEY: "lin_api_xxxxx"
    context: "Connected to Linear workspace. Use to list/create issues, search projects."
```

### Git (Repo Analysis)

```yaml
mcp_servers:
  git:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-git"]
    context: "Use git_log, git_diff, git_show for repository analysis."
```

### HTTP Transport (Remote MCP Server)

```yaml
mcp_servers:
  my-remote-server:
    type: http
    url: "https://mcp.example.com/sse"
    context: "Remote MCP server providing custom tools."
```

### Multiple Servers

You can run multiple MCP servers simultaneously:

```yaml
mcp_servers:
  github:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "ghp_xxxxx"
    context: "GitHub user 'yourname'. query 'user:yourname' for repos."
  slack:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-slack"]
    env:
      SLACK_BOT_TOKEN: "xoxb-xxxxx"
      SLACK_TEAM_ID: "T01234567"
    context: "Slack workspace 'MyTeam'."
  postgres:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-postgres", "postgresql://localhost/mydb"]
    context: "PostgreSQL mydb."
```

### MCP Client Notes

- **`context` is critical** — tells the LLM who's authenticated and what queries to use. Without it, the LLM guesses wrong.
- **All MCP tools require approval**. Use `shan -y` for auto-approve in one-shot mode.
- **Local tools take priority** — if an MCP tool has the same name as a local tool, the local one wins.
- **Project-level overrides** — put server configs in `.shannon/config.yaml` (project) or `.shannon/config.local.yaml` (gitignored).
- **One-shot vs interactive** — each `shan "query"` starts fresh MCP connections. In interactive TUI mode (`shan`), connections persist for the session.
- Find more servers at [MCP Server Registry](https://registry.modelcontextprotocol.io/) and [Awesome MCP Servers](https://github.com/punkpeye/awesome-mcp-servers).

## Configuration

### Multi-level Config

Config files are merged in order (later overrides earlier):

1. `~/.shannon/config.yaml` — global
2. `.shannon/config.yaml` — project
3. `.shannon/config.local.yaml` — local override (gitignored)

Merge behavior: scalars override, lists merge + dedup, structs field-level merge.

### Full Config Structure

```yaml
# Connection
endpoint: http://localhost:8080    # Shannon Gateway URL
api_key: ""                        # Gateway API key
model_tier: medium                 # small, medium, large

# Permissions
permissions:
  allowed_dirs:
    - ~/Documents/notes
    - ./docs
  allowed_commands:
    - "git *"
    - "go test *"
    - "make *"
  denied_commands:
    - "rm -rf *"
  network_allowlist:
    - "localhost"
    - "127.0.0.1"
    - "api.example.com"

# MCP servers (external tool sources)
mcp_servers:
  github:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "ghp_xxxxx"
    context: "GitHub user 'yourname'."

# Agent behavior
agent:
  max_iterations: 25               # max tool calls per turn

# Tool settings
tools:
  bash_timeout: 120                # seconds
  result_truncation: 2000          # max chars in tool result
  args_truncation: 200             # max chars in displayed args

# Hooks
hooks:
  PreToolUse:
    - matcher: "bash"
      command: ".shannon/hooks/check-bash.sh"
  PostToolUse:
    - matcher: "file_edit|file_write"
      command: ".shannon/hooks/post-edit.sh"
  SessionStart:
    - command: ".shannon/hooks/on-start.sh"
  Stop:
    - command: ".shannon/hooks/on-stop.sh"
```

Use `/config` in the TUI to see the merged config with sources showing which file each value came from.

### UI Settings — `~/.shannon/settings.json`

```json
{
  "spinner_texts": [
    "Thinking deeply...",
    "Exploring possibilities...",
    "Connecting the dots..."
  ]
}
```

## Instructions & Memory

### Instructions

AI behavior customization loaded from markdown files:

- `~/.shannon/instructions.md` — global instructions
- `.shannon/instructions.md` — project instructions

Both are loaded into the system prompt (token-budgeted, deduplicated).

### Persistent Memory

- `~/.shannon/memory/MEMORY.md` — first 200 lines loaded on startup
- The agent can write to this file to remember information across sessions

### Custom Slash Commands

Create `.shannon/commands/<name>.md` or `~/.shannon/commands/<name>.md`:

```markdown
Review the following code for bugs and security issues.
Focus on: $ARGUMENTS
```

This creates a `/name` command in the TUI. `$ARGUMENTS` is replaced with whatever follows the command.

## Sessions

Conversations are persisted as JSON files in `~/.shannon/sessions/`.

- Each session is a `<id>.json` file containing messages, metadata, and remote task IDs
- Saved after each agent turn and on exit
- Titles generated from the first user message (truncated to 50 chars)

```
/sessions                              # interactive picker
/session resume 1                      # by number
/session resume 2026-02-23-a1b2c3      # by full ID
/session new                           # start fresh
```

## SSE Event Handling

Remote workflows (`/research`, `/swarm`) stream events via SSE:

| Event | Display |
|-------|---------|
| `WORKFLOW_STARTED` | `> Starting workflow...` |
| `PROGRESS`, `STATUS_UPDATE` | `> Processing...` |
| `AGENT_STARTED` | `> Agent working...` |
| `TOOL_INVOKED`, `TOOL_STARTED` | `? Calling tool...` |
| `thread.message.delta` | Streaming text (incremental) |
| `thread.message.completed` | Final response |
| `WORKFLOW_FAILED`, `error` | `! Error: ...` |

## UI Behavior

- **Inline terminal rendering** (no alt screen) — allows normal mouse text selection
- **Scrollable viewport** with Up/Down/PgUp/PgDn
- **Slash command menu**: appears on `/`, filters as you type, Tab/Enter to select
- **Session picker**: navigable list with Up/Down
- **Token usage**: `[tokens: N | cost: $X.XXXX]` after each response

## Keyboard

| Key | Context | Action |
|-----|---------|--------|
| Up/Down | Output | Scroll viewport |
| Up/Down | Command menu | Navigate items |
| Tab/Enter | Command menu | Insert selected command |
| Enter | Input | Submit message |
| Escape | Menu/picker | Close |
| y/n | Approval prompt | Approve/deny tool call |
| Ctrl+C | Any | Save session and exit |

## Project Structure

```
shannon-cli/
  main.go                           Entry point
  cmd/root.go                       Cobra CLI, one-shot mode, MCP serve subcommand
  internal/
    agent/
      loop.go                       Agent loop with permissions, hooks, audit
      tools.go                      Tool/SafeChecker interfaces, ToolRegistry
    client/
      gateway.go                    Gateway client (completions, tools, tasks)
      sse.go                        SSE parser (spec-compliant, 4MB buffer)
    config/
      config.go                     Multi-level YAML config (global/project/local)
      settings.go                   UI settings (JSON)
    session/
      store.go                      JSON file persistence
      manager.go                    Session lifecycle, ID generation
    permissions/
      permissions.go                5-layer permission engine, hard-block, symlink protection
    audit/
      audit.go                      JSON-lines logger, auto-redact secrets
    hooks/
      hooks.go                      PreToolUse/PostToolUse/SessionStart/Stop
    instructions/
      loader.go                     Instructions, memory, custom command loading
    prompt/
      builder.go                    5-layer token-budgeted system prompt builder
    mcp/
      server.go                     MCP server (JSON-RPC 2.0 over stdio)
      client.go                     MCP client manager (connect to external servers)
    tools/
      file_read.go                  Read with line numbers
      file_write.go                 Write with mkdir -p
      file_edit.go                  Unique string replacement
      bash.go                       Shell exec with SafeChecker
      grep.go                       ripgrep/grep fallback
      glob.go                       doublestar recursive patterns
      directory_list.go             Directory listing
      http.go                       HTTP client with network allowlist
      system_info.go                OS/CPU/memory/disk info
      clipboard.go                  pbcopy/pbpaste
      notify.go                     macOS notifications
      process.go                    ps/lsof/kill
      applescript.go                Arbitrary AppleScript
      browser.go                    Chromedp isolated Chrome
      screenshot.go                 macOS screencapture
      computer.go                   Mouse/keyboard via Quartz/System Events
      mcp_tool.go                   MCP tool adapter (wraps remote tools)
      register.go                   Tool registration (local + MCP + gateway)
    tui/
      app.go                        Bubbletea TUI
    update/
      selfupdate.go                 GitHub release self-update
  test/
    integration_test.go             E2E test with mock server
```

## Building & Testing

```bash
go build -o shan .           # build
go test ./...                # run all tests
go vet ./...                 # lint
```

## License

MIT
