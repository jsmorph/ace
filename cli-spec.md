# ACE CLI

This document specifies the `ace` command-line interface. See
`spec.md` for the core operations, pattern matching, access
control, TTL, blocking, and limits.

All subcommands accept `--db` (default: `ace.db`) to specify
the SQLite database file.

The `out`, `in`, `rd`, `del`, and `stats` subcommands accept
`--server` to specify an ACE server URL (e.g.,
`http://localhost:8000`). If `--server` is not given, the
`$ACE_URL` environment variable is used when set. When a
server URL is active, the command sends HTTP requests to the
server instead of opening a local database, and `--db` is
ignored.

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
| `--visibility-timeout` | `PT30S` | Visibility timeout (ISO 8601) |

The server deletes expired objects at the scavenge interval.

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
| `--id` | (none) | Caller identity for access control |
| `--wait` | (none) | Block duration (integer seconds, ISO 8601, or Go duration) |
| `--deletes` | false | Enable explicit deletes |

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

Output:

```json
{"match": true}
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

## ace expire

Delete all expired objects and report the count.

```
deleted 7 expired objects
```

## ace help

Print a summary of ACE's functionality, operations, and how
to access the full documentation. The output is the contents
of `skill.md`.

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
| `--blocking` | `notify` | Blocking implementation |
| `--max-waiters` | 0 | Max concurrent blocking clients |
