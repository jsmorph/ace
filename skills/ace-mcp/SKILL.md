# ACE Remote MCP

Use this skill when an agent accesses ACE through MCP over Streamable HTTP. This skill covers `POST /mcp` only. Use the ACE CLI skill or ACE HTTP API skill for non-MCP access.

## Endpoint

Remote MCP runs on the ACE HTTP server:

```text
https://HOST/mcp
```

Send one JSON-RPC message per HTTP `POST`. A request with an `id` returns one JSON-RPC response with `Content-Type: application/json`. A notification or client response returns `202 Accepted` with no body; `GET /mcp` returns 405 because ACE does not send unsolicited server messages.

Send these headers:

```http
Accept: application/json, text/event-stream
Content-Type: application/json
MCP-Protocol-Version: 2025-06-18
Authorization: Bearer TOKEN
```

`Authorization` is required when the server uses `--mcp-token` or `ACE_MCP_TOKEN`. Browser clients must send an allowed `Origin` when the server uses `--mcp-origins`. ACE accepts protocol versions `2024-11-05`, `2025-03-26`, and `2025-06-18`; send `2025-06-18` unless the MCP client requires an older revision.

## Request Body

Every tool call uses this JSON-RPC request body:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "ace_out",
    "arguments": {}
  }
}
```

Objects like `{"name":"ace_out","arguments":{}}` are `params`, not complete HTTP request bodies. ACE operation failures return a tool result with `isError: true`; malformed JSON-RPC, unknown methods, and unknown tool names return JSON-RPC errors. Inspect `result.isError` before retrying a call.

## Setup

Initialize the session:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}
```

List tools:

```json
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
```

ACE declares `tools.listChanged: false`. Reuse the tool list during a run. Send one JSON-RPC message per HTTP request.

## ACE Model

ACE stores JSON objects in a persistent tuple space. `ace_out` writes an object, `ace_in` claims and consumes the earliest matching object, and `ace_rd` reads without consuming. A pattern is a JSON object whose fields must match; arrays mean alternatives, and extra fields in a stored object do not prevent a match.

Use matched fields for routing:

```json
{"type":"task","queue":"review","status":"ready"}
```

Use `#`-prefixed fields for metadata returned with the object but ignored by matching:

```json
{"type":"task","queue":"review","#trace":"run-42"}
```

## Tool Arguments

These objects go under `params.arguments` in a `tools/call` request.

| Tool | Arguments |
|------|-----------|
| `ace_out` | `{"object":{...},"access":{"in":["ace:..."],"rd":["ace:..."]},"ttl":"PT2H"}` |
| `ace_in` | `{"pattern":{...},"wait":"PT30S","since":"2026-06-05T14:22:09.123456789","client_key":"..."}` |
| `ace_rd` | Same as `ace_in`; the object remains visible |
| `ace_del` | `{"delete_id":"..."}` |
| `ace_stats` | `{}` |
| `ace_reg` | `{"name":"worker"}` |
| `ace_regcheck` | Exactly one of `{"key":"..."}`, `{"id":"ace:..."}`, or `{"name":"acen:worker"}` |
| `ace_match` | `{"object":{...},"pattern":{...}}` |

`wait` accepts a number of seconds, an integer string, a Go duration string, or an ISO 8601 duration string. `client_key` resolves through ACE identity registration and takes precedence over `caller_id`. Use `caller_id` only when the server allows insecure IDs and the deployment accepts that risk.

## Write

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "ace_out",
    "arguments": {
      "object": {"type":"task","queue":"review","status":"ready"},
      "ttl": "PT2H"
    }
  }
}
```

The JSON-RPC result contains the ACE result:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [{"type":"text","text":"{\"id\":\"2026-06-05T14:22:09.123456789\"}"}],
    "structuredContent": {"id":"2026-06-05T14:22:09.123456789"}
  }
}
```

## Claim or Read

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "ace_in",
    "arguments": {
      "pattern": {"type":"task","queue":"review","status":"ready"},
      "wait": "PT30S",
      "client_key": "..."
    }
  }
}
```

`ace_in` and `ace_rd` return `{"result":null}` when no object matches before the deadline. A match returns `{"result":{"id":"...","object":{...}}}` under `structuredContent`. When explicit deletes are enabled, an `ace_in` match also includes `delete_id`; call `ace_del` after the agent completes the claimed work.

## Identity

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "tools/call",
  "params": {
    "name": "ace_reg",
    "arguments": {"name":"worker"}
  }
}
```

The result contains `key`, `id`, and `name`. Use `key` as `client_key` for restricted `ace_in` and `ace_rd` calls. Use `ace_regcheck` when an agent needs to validate a key, ID, or name before writing restricted work.

## Matching

```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "method": "tools/call",
  "params": {
    "name": "ace_match",
    "arguments": {
      "object": {"type":"task","queue":"review"},
      "pattern": {"type":"task"}
    }
  }
}
```

Embedding predicates use `~`, as in `{"context~":"TexMex food"}`. LLM predicates use `?`, as in `{"comment?":"TexMex food"}`. Use exact fields for queues, task types, status, ownership, and capabilities.

## Use

Use bounded waits on `ace_in` instead of repeated immediate calls. Keep objects and patterns small, and put audit text in `#` fields. Treat `{"result":null}` as no matching work, not a transport failure.
