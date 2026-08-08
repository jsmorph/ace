# ACE CLI

Use this skill when an agent coordinates work through ACE with the `ace` command. The CLI can operate directly on a local SQLite database, or it can call a remote ACE HTTP server when `--server` or `ACE_URL` is set. The core model is the same in both modes: agents write JSON objects with `out`, claim work with `in`, observe state with `rd`, and confirm explicit deletes with `del` when the server uses visibility timeouts.

## Core Model

ACE is a persistent JSON tuple space. A producer writes an object without naming a recipient. A consumer supplies a JSON pattern and receives the earliest matching object. Extra fields in an object do not block a match, so objects should carry enough typed fields for precise selection and enough unmatched metadata for auditing.

Use matched fields for routing and selection:

```json
{"type":"task","queue":"review","status":"ready","priority":2}
```

Use `#`-prefixed fields for metadata returned with the object but ignored by matching:

```json
{"type":"task","queue":"review","#trace":"run-42","#note":"created by planner"}
```

Patterns are JSON objects. All fields in the pattern must match. Arrays mean alternatives:

```json
{"type":"task","queue":["review","repair"],"status":"ready"}
```

The empty pattern `{}` matches any visible object. Avoid broad patterns when several agents share a space; broad matches consume unrelated work.

## Local and Remote Modes

Local mode opens a SQLite database:

```sh
ace out --db ace.db --object '{"type":"task","queue":"review","status":"ready"}'
ace in --db ace.db --pattern '{"type":"task","queue":"review","status":"ready"}'
```

Remote mode sends HTTP requests to an ACE server:

```sh
export ACE_URL=http://localhost:8000
ace out --object '{"type":"task","queue":"review","status":"ready"}'
ace in --pattern '{"type":"task","queue":"review","status":"ready"}'
```

Remote mode ignores local database flags. Server configuration controls blocking, explicit deletes, embeddings, LLM predicates, and identity validation.

## Writing Objects

`--object` writes one JSON object.  `--kv` builds one object
from comma-separated `key=value` fields and stores each value
as a JSON string.  These input forms are mutually exclusive.

```sh
ace out --object '{"type":"task","queue":"build","status":"ready","cmd":"make test"}'
ace out --kv type=task,queue=build,status=ready
```

Write many objects from stdin, one JSON object per line:

```sh
printf '%s\n' \
  '{"type":"task","queue":"build","status":"ready","cmd":"go test ./..."}' \
  '{"type":"task","queue":"docs","status":"ready","file":"README.md"}' |
ace out
```

Set a TTL when work should expire:

```sh
ace out --ttl PT2H --object '{"type":"task","queue":"review","status":"ready"}'
```

Objects default to a 72-hour TTL. The default maximum TTL is seven days.

## Claiming and Reading

Use `in` to claim work.  It removes the matching object unless
explicit deletes are enabled.  Supply a JSON pattern through
`--pattern` or string-valued fields through `--kv`.

```sh
ace in --pattern '{"type":"task","queue":"review","status":"ready"}'
ace in --kv type=task,queue=review,status=ready
```

Use `rd` to observe without consuming.  It leaves the matched
object in the space.  It accepts the same `--pattern` and
`--kv` input forms as `in`.

```sh
ace rd --pattern '{"type":"state","component":"worker-7"}'
ace rd --kv type=state,component=worker-7
```

Use `--wait` when an agent should block for work instead of polling:

```sh
ace in --wait PT30S --pattern '{"type":"task","queue":"review","status":"ready"}'
```

`--wait` accepts seconds, Go durations, and ISO 8601 durations. A command returns `null` when no object matches before the deadline. Prefer bounded waits so agents can refresh configuration, check cancellation, and write status records.

Use `--since` to resume a scan after a known ACE identifier:

```sh
ace rd --since 2026-06-05T14:22:09.123456789 --pattern '{"type":"event"}'
```

## Explicit Deletes

When explicit deletes are enabled, `in` returns a `delete_id` instead of removing the object immediately. The object becomes invisible until the visibility timeout expires. Confirm completion with `del`:

```sh
result=$(ace in --deletes --pattern '{"type":"task","queue":"build","status":"ready"}')
delete_id=$(printf '%s' "$result" | jq -r .delete_id)
ace del --delete-id "$delete_id"
```

If an agent crashes before `del`, the object reappears after the visibility timeout. Use explicit deletes for task claiming. They are unnecessary for read-only observation.

## Identity and Access

Register an identity:

```sh
ace reg --name worker
```

Use the returned key for authenticated access:

```sh
export ACE_CLIENT_KEY=...
ace in --key "$ACE_CLIENT_KEY" --pattern '{"type":"task","queue":"private"}'
```

Restrict a written object:

```sh
ace out \
  --access '{"in":["ace:worker-id"],"rd":["ace:monitor-id"]}' \
  --object '{"type":"task","queue":"private","status":"ready"}'
```

Use identity keys rather than bare IDs in remote mode. Bare IDs require the server to allow insecure IDs.

## Dynamic Predicates

Embedding predicates use `~` and compare text semantically:

```sh
ace rd --pattern '{"type":"note","context~":"TexMex food"}'
```

LLM predicates use `?` and ask whether a field relates to the pattern text:

```sh
ace rd --pattern '{"type":"note","comment?":"TexMex food"}'
```

These predicates may call external model endpoints. Use them only when exact fields cannot express the match. Prefer exact routing fields for queues, task types, ownership, and status.

## Agent Patterns

Use stable `type`, `queue`, and `status` fields. Keep patterns narrow enough that a worker cannot claim another worker's task. Put audit data and large notes under `#` fields so they do not expand the match index. Use `rd` for state inspection and `in` only when the agent will act on the claimed object.

For task coordination, write tasks with `status:"ready"`, claim with `in`, write a result object after completion, and confirm with `del` when explicit deletes are active. For state records, write replaceable objects with short TTLs or include monotonic version fields. For event scans, use `rd --since` to read events without consuming them.

## Useful Commands

| Command | Use |
|---------|-----|
| `ace out` | Write one or more objects |
| `ace in` | Claim and consume one matching object |
| `ace rd` | Read one matching object without consuming it |
| `ace del` | Confirm an explicit-delete claim |
| `ace stats` | Inspect object, branch, and access counts |
| `ace reg` | Create an identity and client key |
| `ace regcheck` | Look up an identity |
| `ace match` | Test object/pattern matching without a database |
| `ace doc` | Read embedded documentation |
