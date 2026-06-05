# ACE CLI

This document specifies the `ace` command-line interface. See
`spec.md` for the core operations, pattern matching, access
control, TTL, blocking, and limits.

Subcommands that open a database accept `--db` (default:
`ace.db`) to specify the SQLite file.  Subcommands that do
not use a database (`match`, `embcmp`, `ping`, `version`,
`test`, `doc`, `help`) ignore `--db`.  The `serve` command
opens the HTTP interface, and the `mcp` command opens the
MCP stdio interface.

The `out`, `in`, `rd`, `del`, `stats`, `reg`, `regcheck`,
`ping`, and `version` subcommands accept `--server` to
specify an ACE server URL (e.g., `http://localhost:8000`).
If `--server` is not given, the `$ACE_URL` environment
variable is used when set.  When a server URL is active, the
command sends HTTP requests to the server instead of opening
a local database, and `--db` is ignored.

## ace serve

Start the HTTP server.

| Flag | Default | Purpose |
|------|---------|---------|
| `--addr` | `localhost:8000` | Listen address |
| `--db` | `ace.db` | Database file |
| `--limits` | (none) | JSON file overriding default limits |
| `--blocking` | `notify` | `polling` or `notify` |
| `--scavenge` | `PT1H` | Expiration cleanup interval (ISO 8601) |
| `--max-waiters` | 0 | Max blocking clients; 0 = unlimited |
| `--deletes` | false | Enable explicit deletes |
| `--embeddings-url` | (default endpoint) | Embeddings endpoint URL for embeddings-based matches |
| `--llm-url` | (default endpoint) | LLM endpoint URL for `?` predicates |
| `--llm-model` | `gpt-5-mini` | LLM model for `?` predicates |
| `--visibility-timeout` | `PT30S` | Visibility timeout (ISO 8601) |
| `--insecure-ids` | false | Allow bare `X-ACE-ID` header without key authentication |
| `--identity-ttl` | `P40D` | Identity expiration (ISO 8601) |
| `--throttle` | 60 | Max requests per minute per IP (0 = unlimited) |
| `--tls` | (none) | Hostname for automatic TLS via Let's Encrypt |
| `--tls-cache` | `certs` | Directory for stored TLS certificates |
| `--updates` | (none) | Update source: GitHub releases URL or local directory |
| `--update-interval` | `PT1H` | Update check interval (ISO 8601) |
| `--mcp-token` | `$ACE_MCP_TOKEN` | Bearer token required for `/mcp` when non-empty |
| `--mcp-origins` | (none) | Comma-separated allowed `Origin` values for `/mcp` |

The server deletes expired objects at each scavenge interval
and purges expired identities with 1-in-10 probability per
tick.

The server also exposes remote MCP at `/mcp` using the
Streamable HTTP transport. `POST /mcp` accepts one MCP
JSON-RPC message and returns a JSON-RPC response or 202 for
notifications and client responses. `GET /mcp` returns 405
because ACE does not send unsolicited MCP messages.
Set `--mcp-token` or `ACE_MCP_TOKEN` to require bearer-token
authentication. Set `--mcp-origins` to reject browser
requests whose `Origin` header is not in the configured list.

When `--tls` is set, the server listens on port 443 for HTTPS
and port 80 for ACME challenges and HTTP-to-HTTPS redirects.
The `--addr` flag is ignored in this mode.  Certificates are
obtained automatically from Let's Encrypt and stored in the
`--tls-cache` directory.

When `--updates` is set, the server polls for a new binary at
the specified interval.  The source is a GitHub releases URL
(fetches `URL/latest/download/ace-GOOS-GOARCH`) or a local
directory containing the binary.  On first check the server
records the current ETag/modtime as a baseline.  When a change
is detected, the server downloads the new binary, stops
accepting new requests (503), waits up to 10 seconds for
in-flight requests to complete, closes the database, starts
the new binary with the same arguments, and exits.  The new
process retries its listen call for up to 30 seconds while
the old process finishes.

## ace mcp

Start the MCP stdio server. The command reads MCP JSON-RPC
messages from stdin and writes MCP JSON-RPC messages to
stdout. Logs and fatal errors are written to stderr.

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

See `mcp-spec.md` for tool names, arguments, and result
formats.

## ace out

Write an object into the space. If `--object` is given,
write that single object. Otherwise read stdin line by line,
treating each line as a separate JSON object. The command
skips blank lines.

| Flag | Default | Purpose |
|------|---------|---------|
| `--server` | `$ACE_URL` | ACE server URL |
| `--object` | (stdin) | JSON object |
| `--access` | (none) | Access control JSON |
| `--ttl` | (none) | TTL as ISO 8601 duration |

Each object produces one line of output:

```json
{"id": "2025-07-14T22:31:05.123456789"}
```

## ace in

Find and remove the earliest matching object.

| Flag | Default | Purpose |
|------|---------|---------|
| `--server` | `$ACE_URL` | ACE server URL |
| `--pattern` | (stdin) | JSON pattern |
| `--since` | (none) | Only objects after this identifier |
| `--id` | (none) | Caller identity for access control (requires `--insecure-ids` in remote mode) |
| `--key` | `$ACE_CLIENT_KEY` | Client key for authentication |
| `--wait` | (none) | Block duration (integer seconds, ISO 8601, or Go duration) |
| `--deletes` | false | Enable explicit deletes |
| `--embeddings-url` | (default endpoint) | Embeddings endpoint URL for `~` predicates |
| `--llm-url` | (default endpoint) | LLM endpoint URL for `?` predicates |
| `--llm-model` | `gpt-5-mini` | LLM model for `?` predicates |

If `--pattern` is absent or `-`, the command reads the
pattern from stdin. Output is a JSON result or `null` if no
match exists. If `--wait` is set and no match exists
immediately, the command blocks until a match appears or the
deadline passes. When `--deletes` is set, the result includes
a `delete_id` field.

## ace rd

`ace rd` uses the same flags as `ace in`. The object remains
in the space.

## ace match

Test whether an object matches a pattern. This command does
not open a database.

| Flag | Default | Purpose |
|------|---------|---------|
| `--object` | (required) | JSON object |
| `--pattern` | (required) | JSON pattern |
| `--embeddings-url` | (default endpoint) | Embeddings endpoint URL |
| `--llm-url` | (default endpoint) | LLM endpoint URL |
| `--llm-model` | `gpt-5-mini` | LLM model |

Output:

```json
{"match": true}
```

## ace embcmp

Compare two strings with the same embeddings client and
distance metrics used by embeddings-based pattern matching.
This command does not open a database. The `distance` field
and `match` decision use the selected metric. The output also
reports cosine similarity, dot product, and both Euclidean
distances for the same pair.

| Flag | Default | Purpose |
|------|---------|---------|
| `--query` | (required) | Query text |
| `--text` | (required) | Candidate text |
| `--metric` | `cosine` | Distance metric: `cosine`, `euclidean`, or `sqeuclidean` |
| `--threshold` | `0.25` | Threshold used to compute the `match` field |
| `--embeddings-url` | (default endpoint) | Embeddings endpoint URL |

Output:

```json
{"query":"TexMex food","object":"tacos and queso","metric":"cosine","distance":0.4428172711546092,"cosine_distance":0.4428172711546092,"cosine_similarity":0.5571827288453908,"dot_product":0.8173496184375029,"euclidean_distance":0.9410810769616223,"squared_euclidean_distance":0.8856331227684698,"threshold":0.25,"match":false}
```

## ace del

Confirm deletion of an object previously returned by
`ace in --deletes`. See `spec.md` for the explicit deletes
mechanism.

| Flag | Default | Purpose |
|------|---------|---------|
| `--server` | `$ACE_URL` | ACE server URL |
| `--delete-id` | (required) | Deletion ID from `ace in` |

Output:

```json
{"deleted": true}
```

## ace stats

Print storage statistics as JSON. See `http-spec.md` for
the fields.

| Flag | Default | Purpose |
|------|---------|---------|
| `--server` | `$ACE_URL` | ACE server URL |

## ace reg

Register a new client identity. See `spec.md` for the
identity model.

| Flag | Default | Purpose |
|------|---------|---------|
| `--server` | `$ACE_URL` | ACE server URL |
| `--name` | (none) | Human-readable name |

Output:

```json
{"key": "a1b2c3...64hex", "id": "ace:d4e5f6...64hex", "name": "acen:alice"}
```

## ace regcheck

Look up a registered identity by key, ID, or name. Provide
exactly one of the three lookup methods. The response
contains only the fields appropriate to the lookup method.

| Flag | Default | Purpose |
|------|---------|---------|
| `--server` | `$ACE_URL` | ACE server URL |
| `--key` | `$ACE_CLIENT_KEY` | Client key |
| `--id` | (none) | Identity ID |
| `--name` | (none) | Identity name |

| Lookup by | Returns |
|-----------|---------|
| `--key` | `id` and `name` |
| `--id` | `name` |
| `--name` | `id` |

Example output for key lookup:

```json
{"id": "ace:d4e5f6...64hex", "name": "acen:alice"}
```

## ace ping

Print the server's commit hash.  When `--server` is given (or
`$ACE_URL` is set), queries the remote `/ping` endpoint.
Otherwise prints the commit hash embedded in the local binary.

| Flag | Default | Purpose |
|------|---------|---------|
| `--server` | `$ACE_URL` | ACE server URL |

Output:

```json
{"commit": "a1b2c3d4..."}
```

## ace version

Alias for `ace ping`.

## ace expire

Delete all expired objects and expired identities, and report
the counts.

```
deleted 7 expired objects
deleted 0 expired identities
```

## ace help

Print an agent-facing guide for using ACE through the CLI.
The output is the contents of `skills/ace-cli/SKILL.md`.

## ace doc

Print embedded documentation. With no arguments, list the
available documents. With a filename argument, print that
document.

```
ace doc
ace doc spec.md
```

## ace test

Run the built-in stress test over HTTP. See `README.md` for
details.

| Flag | Default | Purpose |
|------|---------|---------|
| `--writers` | 4 | Concurrent writer goroutines |
| `--readers` | 4 | Concurrent reader goroutines |
| `--requests` | 100 | Requests per writer |
| `--wait` | 2 | Blocking read timeout (seconds) |
| `--blocking` | `notify` | Blocking implementation |
| `--max-waiters` | 0 | Max concurrent blocking clients |
