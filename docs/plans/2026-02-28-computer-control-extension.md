# Computer Control Extension — Implementation Record

Date: 2026-02-28

## What Was Built

Shannon CLI extended from a code-focused agent (7 tools) to a local computer control platform (16 tools + 6 new packages), following the plan at `~/Desktop/shannon-cli-computer-control-plan.md`.

## New Packages

### `internal/permissions/` — Permission Engine
- Hard-block constants (rm -rf /, curl|sh, dd of=/dev — cannot be overridden)
- Denied/allowed command matching with glob wildcards
- Compound shell command parsing (splits on &&, ||, ;, | — checks each sub-command)
- Symlink-safe file path checking (`filepath.EvalSymlinks` before allowed_dirs check)
- Sensitive file detection (.env, *.pem, id_rsa, etc. — blocked even in allowed_dirs)
- Network egress allowlist (localhost default-ok, external domains need explicit allowlist)

### `internal/audit/` — Audit Logger
- Append-only JSON-lines at `~/.shannon/logs/audit.log`
- Every tool call logged: name, args, output, decision, approval, duration
- Auto-redacts secrets: AWS keys, JWT, Bearer tokens, PEM, sk-/key- prefixes, env var assignments with KEY/SECRET/TOKEN/PASSWORD

### `internal/instructions/` — Instruction & Memory Loader
- Multi-level instruction files: `~/.shannon/instructions.md` (global) → `.shannon/instructions.md` (project) → `.shannon/instructions.local.md` (local)
- `~/.shannon/rules/*.md` and `.shannon/rules/*.md` auto-loaded
- Line deduplication across levels
- `LoadMemory()`: reads `~/.shannon/memory/MEMORY.md` (first 200 lines)
- `LoadCustomCommands()`: scans `~/.shannon/commands/*.md` and `.shannon/commands/*.md`

### `internal/prompt/` — System Prompt Builder
- 5-layer token-budgeted assembly: base → memory → instructions → tools → context
- Each layer has independent character budget (memory: 2000, instructions: 16000, context: 800)
- Replaces hardcoded system prompt and `knownLocalTools` list
- Tool names auto-generated from ToolRegistry

### `internal/hooks/` — Lifecycle Hooks
- Events: PreToolUse, PostToolUse, SessionStart, Stop
- Shell script protocol: JSON via stdin, exit codes determine behavior (0=allow, 2=deny)
- Sandboxed: 10s timeout, 10KB output limit, path restriction, recursion guard
- Regex matcher for tool name filtering

### `internal/mcp/` — MCP Server
- JSON-RPC 2.0 over stdio
- Methods: initialize, tools/list, tools/call
- Converts ToolRegistry to MCP tool definitions
- Command: `shan mcp serve`

## New Tools (9)

| Tool | Purpose | RequiresApproval |
|---|---|---|
| `http` | HTTP client (GET/POST/PUT/DELETE) | yes (SafeChecker: localhost GET auto-approve) |
| `system_info` | OS/arch/memory/disk/CPU | no |
| `clipboard` | pbcopy/pbpaste read/write | yes |
| `notify` | macOS desktop notifications via osascript | yes |
| `process` | ps/lsof/kill | yes (SafeChecker: list/ports auto-approve) |
| `applescript` | arbitrary AppleScript execution | yes |
| `browser` | chromedp browser automation (isolated profile) | yes |
| `screenshot` | macOS screencapture (fullscreen/window/region) | yes |
| `computer` | OS-level mouse/keyboard (click/type/hotkey/move) | yes |

## Config Changes

### Expanded Config Struct
```yaml
permissions:
  allowed_dirs: [~/Documents/notes, ./docs]
  allowed_commands: ["git *", "go test *"]
  denied_commands: ["rm -rf *"]
  sensitive_patterns: [".env", "*.pem"]
  network_allowlist: ["localhost", "127.0.0.1"]

agent:
  max_iterations: 25
  temperature: 0
  max_tokens: 0

tools:
  bash_timeout: 120
  bash_max_output: 30000
  result_truncation: 2000
  args_truncation: 200
  server_tool_timeout: 5
  grep_max_results: 100

hooks:
  PreToolUse:
    - matcher: "bash"
      command: ".shannon/hooks/pre-bash.sh"
  PostToolUse:
    - matcher: "file_edit|file_write"
      command: ".shannon/hooks/post-edit.sh"
```

### Multi-Level Config Loading
1. `~/.shannon/config.yaml` (global, lowest priority)
2. `.shannon/config.yaml` (project, team-shared)
3. `.shannon/config.local.yaml` (project local, gitignored, highest priority)

Merge strategy: scalars override, lists merge+dedup, structs field-level merge.

## Agent Loop Wiring

Tool execution flow:
```
LLM returns function_call
  → Permission check (hard-block → denied → shell parse → allowed → default ask)
  → Hook check (PreToolUse — can deny via exit code 2)
  → RequiresApproval + SafeChecker (existing, now second layer)
  → tool.Run()
  → Audit log (name, args, output, decision, duration, redacted)
  → Hook fire (PostToolUse — fire-and-forget)
```

## Browser Strategy

Three tiers (Chrome 136+ blocks remote debugging on real profiles):
- **Tier A**: Automation browser — chromedp + Chrome for Testing, isolated profile (default)
- **Tier B**: OS-level Computer Use — screenshot + mouse/keyboard via Python3+Quartz/AppleScript
- **Tier C**: API connector — MCP servers for Jira/GitHub/Slack (avoids UI)

## Test Coverage

| Package | Coverage |
|---|---|
| prompt | 100% |
| permissions | 95.2% |
| instructions | 95.2% |
| mcp | 93.8% |
| audit | 90.3% |
| hooks | 82.6% |
| agent | 59.2% |
| tools | 54.3% |

## Commits

| Hash | Description |
|---|---|
| `8333f0a` | Phase 1: tools + permissions + audit + instructions + prompt |
| `8ed36a8` | Phase 1: multi-level config + bash from config + /config cmd |
| `e7ce9c5` | Phase 2: browser + hooks + custom command dispatch |
| `2e0053a` | Phase 3: screenshot + computer use + MCP server |

## Phase 4 TODO (cloud/infra dependent)

- Session-level capability grant (`--grant` flag)
- Remote Control (local session, web UI remote window)
- Cloud Sandbox (Firecracker VM)
- Execution target routing (workstation / isolated_mac / cloud_vm)
- Custom tool plugins (`custom_tools` config)
- Vision loop (needs Shannon API image input support)
- MCP Client (connect to third-party MCP servers)
