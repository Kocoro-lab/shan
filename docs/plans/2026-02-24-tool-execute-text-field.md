# Request: Add `text` field to tool execute response

## Problem

The CLI now executes server tools client-side via `POST /api/v1/tools/{name}/execute`. The response returns structured JSON in the `output` field:

```json
{
  "success": true,
  "output": {
    "content": "...(7487 chars)...",
    "title": "The Go Programming Language",
    "snippet": "...(500 chars)...",
    "url": "https://go.dev",
    "word_count": 912,
    "method": "firecrawl",
    ...
  }
}
```

The CLI passes `JSON.stringify(output)` directly to the LLM as the tool result. This causes:

1. **LLM echoes raw JSON** — the model includes fragments of the raw JSON in its response text
2. **Token waste** — full JSON with metadata fields (`method`, `tool_source`, `status_code`, etc.) consumes tokens without helping the LLM answer
3. **Per-tool formatting on CLI is fragile** — each tool has different output structure; the backend knows these best

When `shannon-complex` runs tools server-side, it presumably formats output before passing to the LLM. The CLI needs the same.

## Proposal

Add a top-level `text` field to the tool execute response — a pre-formatted, LLM-friendly text representation:

```json
{
  "success": true,
  "output": { ... },
  "text": "Title: The Go Programming Language\nURL: https://go.dev\n\nThe Go programming language is an open source project...",
  "error": null,
  "execution_time_ms": 1234
}
```

### Per-tool examples

**web_search**:
```
Results for "Wayland Zhang":
1. Wayland Zhang - waylandz.com
   AI researcher and software engineer. Teaching modern AI concepts to 100k+ followers.
2. WAYLAND ZHANG - github.com/waylandzhang
   A production-oriented multi-agent orchestration framework.
...
```

**web_fetch**:
```
Title: The Go Programming Language
URL: https://go.dev

The Go programming language is an open source project to make programmers more productive...
```

**calculator**:
```
4
```

**getStockBars**:
```
AAPL (2026-02-24):
  Open: 245.12, High: 248.50, Low: 244.80, Close: 247.30
  Volume: 52,341,000
```

### Rules

- `text` is the LLM-friendly representation; `output` stays as structured data for programmatic use
- If `text` is null/absent, CLI falls back to `JSON.stringify(output)` (backward compatible)
- Keep `text` concise — strip metadata fields, keep content + key identifiers
- For tools with large content (web_fetch), truncate to a reasonable size (e.g., 3000 chars) in the text field

## CLI changes (already done, waiting on backend)

The CLI will:
1. Use `resp.text` if present
2. Fall back to `string(resp.output)` if absent
3. No per-tool formatting logic needed on CLI side
