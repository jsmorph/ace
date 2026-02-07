# ACE CLI

This document specifies the `ace` command-line interface. See `spec.md` for the core operations, pattern matching, access control, TTL, blocking, and limits.

All subcommands accept `--db` (default: `ace.db`) to specify the SQLite database file.

## ace serve

Start the HTTP server.

| Flag | Default | Purpose |
|------|---------|---------|
| `--port` | `localhost:8000` | Listen address |
| `--db` | `ace.db` | Database file |
| `--limits` | (none) | JSON file overriding default limits |
| `--blocking` | `notify` | Blocking implementation: `polling` or `notify` |
| `--scavenge` | `PT1H` | Interval for deleting expired objects (ISO 8601) |
| `--max-waiters` | 0 | Max concurrent blocking clients; 0 means unlimited |

The server deletes expired objects at the scavenge interval.

## ace out

Write an object into the space. If `--object` is given, write that single object. Otherwise read stdin line by line, treating each line as a separate JSON object. The command skips blank lines.

| Flag | Default | Purpose |
|------|---------|---------|
| `--object` | (stdin) | JSON object |
| `--access` | (none) | Access control JSON |
| `--ttl` | (none) | TTL as ISO 8601 duration |

Each object produces one line of output:

```json
{"id": "2025-07-14T22:31:05.123456789"}
```

## ace in

Find and remove the earliest matching object without blocking.

| Flag | Default | Purpose |
|------|---------|---------|
| `--pattern` | (stdin) | JSON pattern |
| `--since` | (none) | Return only objects after this identifier |
| `--id` | (none) | Caller identity for access control |

If `--pattern` is absent or `-`, the command reads the pattern from stdin. Output is a JSON result or `null` if no match exists.

## ace rd

`ace rd` uses the same flags as `ace in`. The object remains in the space.

## ace stats

Print storage statistics as JSON. See `http-spec.md` for the fields.

## ace expire

Delete all expired objects and report the count.

```
deleted 7 expired objects
```

## ace test

Run the built-in stress test over HTTP. See `README.md` for details.

| Flag | Default | Purpose |
|------|---------|---------|
| `--writers` | 4 | Concurrent writer goroutines |
| `--readers` | 4 | Concurrent reader goroutines |
| `--requests` | 100 | Requests per writer |
| `--blocking` | `notify` | Blocking implementation |
| `--max-waiters` | 0 | Max concurrent blocking clients |
