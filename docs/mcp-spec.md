# ACE MCP Interface

ACE exposes MCP through stdio and remote Streamable HTTP.
`ace mcp` uses stdio, reads one JSON-RPC message per line
from stdin, and writes one JSON-RPC response per line to
stdout. `ace serve` exposes the same MCP tools at `/mcp`.
Both transports use the same JSON-RPC handler and core tool
implementation.

## Protocol

The implementation targets MCP protocol revision `2025-06-18`.  It supports `initialize`, `notifications/initialized`, `ping`, `tools/list`, and `tools/call`.  The server declares the `tools` capability with `listChanged: false`.

Tool calls return MCP `CallToolResult` objects.  Successful calls include a text content block containing compact JSON and a `structuredContent` object with the same data.  ACE operation errors return a tool result with `isError: true`; malformed MCP requests, unknown MCP methods, and unknown tool names return JSON-RPC errors.

## Command

Start a local MCP server:

```sh
ace mcp --db ace.db
```

| Flag | Default | Purpose |
|------|---------|---------|
| `--db` | `ace.db` | Database file |
| `--limits` | (none) | JSON file overriding default limits |
| `--blocking` | `polling` | `polling` or `notify` |
| `--deletes` | false | Enable explicit deletes |
| `--visibility-timeout` | `PT30S` | Visibility timeout (ISO 8601) |
| `--embeddings-url` | (default endpoint) | Embeddings endpoint URL for `~` predicates |
| `--llm-url` | (default endpoint) | LLM endpoint URL for `?` predicates |
| `--llm-model` | `gpt-5-mini` | LLM model for `?` predicates |
| `--insecure-ids` | false | Accepted for configuration parity with other interfaces |
| `--identity-ttl` | `P40D` | Identity expiration (ISO 8601) |

## Remote Endpoint

Start the HTTP server and expose remote MCP:

```sh
ACE_MCP_TOKEN=secret ace serve --addr localhost:8000
```

Remote clients connect to:

```text
http://localhost:8000/mcp
```

Each client message is a new HTTP `POST` request containing a
single MCP JSON-RPC message. Requests receive a JSON-RPC
response with `Content-Type: application/json`. Notifications
and client responses receive `202 Accepted` with no body.
`GET /mcp` returns 405 because ACE does not currently send
unsolicited server-to-client messages.

Recommended request headers:

```http
Accept: application/json, text/event-stream
Content-Type: application/json
MCP-Protocol-Version: 2025-06-18
Authorization: Bearer secret
```

`ace serve --mcp-token TOKEN` requires a bearer token for
`/mcp`; the default comes from `ACE_MCP_TOKEN`.  `ace serve
--mcp-origins https://client.example` restricts browser
requests by the `Origin` header. Use HTTPS for remote
deployments.

## Tools

| Tool | Purpose |
|------|---------|
| `ace_out` | Write a JSON object into the tuple space |
| `ace_in` | Find and remove the earliest matching object |
| `ace_rd` | Find the earliest matching object without removing it |
| `ace_del` | Confirm deletion of an object returned by `ace_in` when explicit deletes are enabled |
| `ace_stats` | Return storage statistics |
| `ace_reg` | Register a client identity |
| `ace_regcheck` | Look up a client identity |
| `ace_match` | Test object/pattern matching without opening a database row |

## Arguments

`ace_out` accepts:

```json
{"object": {"type": "task"}, "access": {"in": ["ace:..."]}, "ttl": "P1D"}
```

`ace_in` and `ace_rd` accept:

```json
{"pattern": {"type": "task"}, "wait": "PT10S", "since": "", "caller_id": "", "client_key": ""}
```

`caller_id` applies access control directly.  `client_key` resolves through the identity table and takes precedence when both fields are present.  `wait` may be a number of seconds, a Go duration string, an integer string, or an ISO 8601 duration string.

`ace_del` accepts `{"delete_id": "..."}`.  `ace_reg` accepts `{"name": "worker"}`.  `ace_regcheck` accepts exactly one of `key`, `id`, or `name`.  `ace_match` accepts `{"object": {...}, "pattern": {...}}`.
