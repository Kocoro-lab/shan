# Shannon CLI Design

**Date:** 2026-02-22
**Status:** Approved
**Repo:** `github.com/Kocoro-lab/shan` (new repo)

## Goal

Build an interactive CLI agent (like Claude Code) for Shannon. Users type `shannon` to enter a conversation loop where they can work with local files, run shell commands, and delegate complex tasks (research, multi-agent swarm) to Shannon's server-side orchestration.

## Key Decisions

| Decision | Choice |
|----------|--------|
| Repo | New repo: `shannon-cli` |
| Language | Go + bubbletea (Charm stack) |
| Binary name | `shannon` (Python SDK renamed to `shannon-sdk`) |
| UX model | `shannon` enters interactive mode, slash commands inside |
| Local agent loop | CLI → `POST /completions/` on Shannon LLM service → local tool execution |
| Remote orchestration | CLI → Gateway API → SSE streaming (for `/research`, `/swarm`) |
| Local storage | JSON files in `~/.shannon/`, no database |
| Self-update | `creativeprojects/go-selfupdate` via GitHub Releases |
| Distribution | `curl \| sh` + GoReleaser + optional Homebrew |

## Architecture

### Two Execution Paths

Both paths are transparent to the user. The CLI routes automatically based on slash commands.

**Path 1: Local Agent Loop (default)**

```
User query
  ↓
POST localhost:8000/completions/
  {messages: [...], tools: [local tool schemas]}
  ↓
LLM returns function_call or text
  ↓
CLI executes tool locally (file/bash/grep)
  ↓
Append result to messages, loop until done
```

Uses Shannon's existing `/completions/` endpoint as "brain only" — it returns tool call decisions without executing them. The CLI handles all local tool execution.

**Path 2: Remote Orchestration (slash commands)**

```
User types /research or /swarm
  ↓
POST localhost:8080/api/v1/tasks/stream
  {query, session_id, context: {force_research|force_swarm}}
  ↓
GET /api/v1/stream/sse?workflow_id=xxx
  ↓
Render agent events, tool observations, final result
```

Uses Shannon's full orchestration pipeline — Temporal workflows, multi-agent coordination, server-side tools.

### Routing Logic

- Default: local agent loop (Path 1)
- `/research [quick|standard|deep] <query>` → remote (Path 2)
- `/swarm <query>` → remote (Path 2). Note: `force_swarm` bypasses decomposition and uses `SwarmWorkflow` directly. It does not interact with `model_tier`, `provider_override`, or `research_strategy` — those are separate routing paths in the orchestrator.
- `shannon "query"` (one-shot from shell) → local agent loop

## Local Tools (MVP)

| Tool | Description | Permission |
|------|-------------|------------|
| `file_read` | Read file with line numbers | Auto-approved |
| `file_write` | Write/overwrite file | Prompt user |
| `file_edit` | old_string → new_string replacement | Prompt user |
| `glob` | Find files by pattern | Auto-approved |
| `grep` | Search file contents (regex) | Auto-approved |
| `bash` | Execute shell command | Prompt (safe-list bypass) |
| `directory_list` | List directory contents | Auto-approved |

**Bash safe-list** (auto-approved): `git status`, `git diff`, `git log`, `go build`, `go test`, `make`, `ls`, `pwd`, `which`, `echo`, `cat`, `head`, `tail`, `wc`.

## Slash Commands (MVP)

| Command | Action |
|---------|--------|
| `/help` | List all commands |
| `/research [quick\|standard\|deep] <query>` | Remote research workflow |
| `/swarm <query>` | Remote swarm multi-agent |
| `/config` | Show/edit config |
| `/sessions` | List, resume, delete sessions |
| `/session new` | Start fresh session |
| `/model [small\|medium\|large]` | Switch model tier |
| `/update` | Self-update binary |
| `/clear` | Clear screen |
| `/quit` | Exit |

## SSE Event Rendering

When in remote orchestration mode, SSE events are rendered as:

| SSE Event | Display |
|-----------|---------|
| `AGENT_STARTED` | `Agent {name} started — {task}` |
| `AGENT_COMPLETED` | `Agent {name} completed` |
| `TOOL_INVOKED` | `⚡ {tool}: {summary}` |
| `TOOL_OBSERVATION` | `→ {truncated result}` |
| `LLM_PARTIAL` | Stream tokens inline |
| `WORKFLOW_COMPLETED` | Show final result |
| `WORKFLOW_FAILED` | Show error |

## Session Management

### Local Storage

```
~/.shannon/
├── config.yaml              # endpoints, api_key, model_tier, auto_update_check
├── sessions/
│   ├── 2026-02-22-a1b2c3.json
│   └── 2026-02-22-d4e5f6.json
└── history                  # readline input history
```

### Session File Format

```json
{
  "id": "a1b2c3",
  "created_at": "2026-02-22T10:30:00Z",
  "title": "Refactoring error handling in main.go",
  "cwd": "/path/to/project",
  "messages": [
    {"role": "user", "content": "..."},
    {"role": "assistant", "content": "...", "tool_calls": [...]},
    {"role": "tool", "name": "file_read", "content": "..."},
    {"role": "assistant", "content": "..."}
  ],
  "remote_tasks": ["wf-xxx"]
}
```

### What Lives Where

| Data | Shannon Server | CLI Local |
|------|---------------|-----------|
| Remote task results | Full history in PostgreSQL | Reference only (`remote_tasks`) |
| Local agent loop messages | Nothing | Full message history |
| Session metadata | Server sessions table | Lightweight JSON |

Conversation context persists across mode switches — research results become context for subsequent local tool calls.

### Session Resume

```
> /sessions
  1. [today]      Refactoring error handling (12 messages)
  2. [yesterday]  Debugging auth middleware (8 messages)

> 1
  Resumed: Refactoring error handling
```

Auto-title generated after first exchange via cheap `/completions/` call.

## Configuration

### Config File (`~/.shannon/config.yaml`)

```yaml
llm_url: "http://localhost:8000"
gateway_url: "http://localhost:8080"
api_key: ""
model_tier: "medium"
auto_update_check: true
```

### First Run

Interactive setup wizard prompts for LLM URL, Gateway URL, and API key. Defaults to localhost for local Docker usage.

### Startup Checks (non-blocking, background)

1. Ping `llm_url/health` — warn if unreachable
2. Ping `gateway_url/health` — warn if unreachable (only affects `/research`, `/swarm`)
3. Check GitHub releases for newer version

## Project Structure

```
shannon-cli/
├── main.go
├── go.mod
├── .goreleaser.yaml
├── install.sh
│
├── cmd/
│   └── root.go                    # Default → interactive, "query" → one-shot
│
├── internal/
│   ├── agent/
│   │   ├── loop.go                # for {} → LLM → tools → repeat
│   │   ├── tools.go               # Tool registry + JSON schemas
│   │   └── permission.go          # Approval prompts, safe-list
│   │
│   ├── tools/
│   │   ├── file_read.go
│   │   ├── file_write.go
│   │   ├── file_edit.go
│   │   ├── bash.go                # Persistent shell session
│   │   ├── grep.go
│   │   ├── glob.go
│   │   └── directory_list.go
│   │
│   ├── client/
│   │   ├── llm.go                 # POST /completions/ client
│   │   ├── gateway.go             # POST /api/v1/tasks/ client
│   │   └── sse.go                 # SSE stream consumer
│   │
│   ├── tui/
│   │   ├── app.go                 # Bubbletea main model
│   │   ├── input.go               # Multi-line input with history
│   │   ├── output.go              # Streaming markdown renderer
│   │   ├── permission.go          # Tool approval dialog
│   │   └── progress.go            # Remote agent progress display
│   │
│   ├── session/
│   │   ├── store.go               # JSON file read/write
│   │   └── manager.go             # Create/resume/list/delete
│   │
│   ├── config/
│   │   └── config.go              # ~/.shannon/config.yaml
│   │
│   └── update/
│       └── selfupdate.go          # go-selfupdate wrapper
│
└── docs/
    └── plans/
```

## Dependencies (Minimal)

| Library | Purpose |
|---------|---------|
| `spf13/cobra` | CLI framework |
| `spf13/viper` | Config loading |
| `charmbracelet/bubbletea` | TUI framework |
| `charmbracelet/glamour` | Markdown rendering |
| `charmbracelet/lipgloss` | Terminal styling |
| `creativeprojects/go-selfupdate` | Self-update |

No SQLite, no ORM, no protobuf dependencies.

## Distribution

- **Install:** `curl -fsSL https://shannon.run/install.sh | bash`
- **Update:** `/update` command (or `shannon --update` from shell)
- **Build:** GoReleaser → GitHub Releases (darwin/arm64, darwin/amd64, linux/arm64, linux/amd64)
- **Optional:** Homebrew tap

## UX Flow

```
$ shannon

  Shannon CLI v0.1.0
  Connected to localhost:8000 (local)
  Session: new session
  Type /help for commands

> find bugs in main.go
  [local agent loop: file_read → analysis → answer]

> /research deep what are the latest error handling patterns in Go
  [remote orchestration: multi-agent research → SSE stream → result]

> apply those patterns to my code
  [local agent loop: file_read → file_edit (approval) → done]

> /quit
  Session saved.
```

## Deferred to v2

- HITL approval gates (`/approve`)
- Schedule management (`/schedule`)
- Agent/skill browsing (`/agents`, `/skills`)
- Advanced TUI (panels, tabs, split views)
- File upload to server workspace
- Cost tracking display (`/cost`)

## References

- OpenCode/Crush (Charm): https://github.com/charmbracelet/crush — Go CLI agent, bubbletea, same local tool pattern
- Claude Code: https://github.com/anthropics/claude-code — reference UX
- go-selfupdate: https://github.com/creativeprojects/go-selfupdate
- Shannon LLM service `/completions/` endpoint: `python/llm-service/llm_service/api/completions.py`
- Shannon Gateway API: `go/orchestrator/cmd/gateway/main.go`
- Shannon streaming: `docs/streaming-api.md`
