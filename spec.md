# ACE Specification

ACE is a coordination service for software agents, built on the tuple-space model: agents communicate by writing JSON objects into a shared persistent store and retrieving them by pattern.

## Objects

An object is a JSON value of type "object" (a set of name-value pairs). Values may be strings, numbers, booleans, null, or nested objects. Object values may not be arrays. Properties whose names start with `#` are metadata: stored and returned but excluded from matching (see Metadata Properties).

ACE stores objects in canonical form: keys in lexicographic order, no HTML escaping. Two JSON objects that differ only in key order or whitespace produce the same canonical representation.

## Identifiers

When an object enters the space, it receives a unique timestamp identifier with nanosecond resolution:

```
YYYY-MM-DDTHH:MM:SS.SSSSSSSSS
```

The time zone is UTC. Identifiers increase monotonically: if two objects arrive within the same nanosecond, the second receives a timestamp one nanosecond after the first. Monotonicity guarantees uniqueness and defines a total order over all objects.

## Operations

ACE provides three operations.

### out

`out(object, access, ttl)` writes an object into the space.

| Parameter | Required | Default | Purpose |
|-----------|----------|---------|---------|
| `object` | yes | | JSON object to store |
| `access` | no | unrestricted | Access control (see Access Control) |
| `ttl` | no | 72 hours | Time-to-live |

ACE validates the object and access against the active limits, canonicalizes the object, assigns an identifier, and stores everything atomically. `out` returns the assigned identifier.

After `out` commits an object, waiting `in` and `rd` operations whose patterns match the new object wake and may return it.

### in

`in(callerID, pattern, wait, since)` finds and removes the earliest matching object.

| Parameter | Required | Default | Purpose |
|-----------|----------|---------|---------|
| `callerID` | no | empty | Caller identity for access control |
| `pattern` | yes | | JSON pattern (see Pattern Matching) |
| `wait` | no | 0 | Maximum seconds to block |
| `since` | no | empty | Return only objects after this identifier |

If a matching object exists, `in` removes it from the space and returns both its identifier and content. If no match exists and `wait` is zero, `in` returns nothing. If `wait` is positive, `in` blocks until a match appears or the deadline passes.

The `since` parameter restricts results to objects with identifiers strictly greater than the given value, enabling cursor-style iteration.

### rd

`rd(callerID, pattern, wait, since)` finds the earliest matching object without removing it. Parameters and behavior match `in` except that the object remains in the space.

### del

`del(deleteID)` permanently deletes an object that `in` previously marked invisible. The `deleteID` is a cryptographic token that `in` returns when explicit deletes are enabled (see Explicit Deletes). `del` returns true if the object was deleted, false if the `deleteID` was invalid or its visibility timeout had already expired.

### Ordering

`in` and `rd` return the matching object with the smallest identifier. The effect is FIFO ordering.

## Pattern matching

A pattern is a JSON object. An object matches a pattern when every root-to-leaf path (branch) in the pattern has a corresponding branch in the object.

```
For every branch B with leaf L in the pattern:
  If L is atomic: B appears in the object.
  If L is an array [X1,...,Xn]: B with leaf Xi appears in the object for some i.
```

An array in a pattern means "any of these values." Extra fields in the object do not prevent a match. The empty pattern `{}` matches every object.

| Pattern | Object | Match? |
|---------|--------|--------|
| `{"a":1}` | `{"a":1}` | yes |
| `{"a":[1,2]}` | `{"a":1}` | yes |
| `{"a":[1,2]}` | `{"a":3}` | no |
| `{"a":[1,2]}` | `{"a":1,"b":0}` | yes |
| `{"b":[1,2]}` | `{"a":1}` | no |
| `{"b":[1,2]}` | `{"a":3,"b":1}` | yes |
| `{"a":{"b":1,"c":2}}` | `{"a":{"b":1,"c":2,"d":3}}` | yes |
| `{"a":{"b":1,"c":2},"d":3}` | `{"a":{"b":1,"c":2,"d":3}}` | no |

The last row fails because the pattern requires `"d":3` at the top level, but the object contains `"d":3` only inside `"a"`.

## Metadata properties

A property whose name starts with `#` is a metadata property. Metadata properties are stored in the object and returned to callers, but they are invisible to pattern matching: no branches are generated for them, and they do not count against the object leaf limit.

The filter applies at every nesting level. In `{"a":{"#note":"x","b":1}}`, the `#note` property is metadata regardless of its position; only `a.b` participates in matching. A top-level `#` property like `{"#id":"abc","type":"task"}` skips the entire subtree rooted at that key.

A `#` property in a pattern is an error. Because metadata properties generate no branches, a pattern that references one can never add useful constraints.

| Object | Pattern | Result |
|--------|---------|--------|
| `{"#tag":"debug","type":"task"}` | `{"type":"task"}` | match |
| `{"#tag":"debug","type":"task"}` | `{}` | match |
| `{"#tag":"debug","type":"task"}` | `{"#tag":"debug"}` | error |

## Access control

The `access` parameter on `out` restricts which callers may retrieve an object. It contains two optional fields:

| Field | Controls |
|-------|----------|
| `in` | Which callers may consume (remove) the object |
| `rd` | Which callers may read the object |

Each field holds a list of caller identity strings. The rules:

1. If an object has no `access`, any caller can retrieve it via `in` or `rd`.
2. If an object has an `access.in` or `access.rd` list, only callers whose identity appears in that list can perform the corresponding operation. Other callers do not see the object for that operation.
3. The two lists are independent. An object can restrict `in` without restricting `rd`, or vice versa.

A caller with no identity (empty `callerID`) can retrieve unrestricted objects but cannot match any access list.

## TTL and expiration

Every object has a time-to-live. If `out` receives no `ttl`, the default is 72 hours. The TTL must be positive and must not exceed the maximum (default: 7 days).

An object's expiration time equals its creation time plus its TTL. `in` and `rd` skip expired objects. Periodic cleanup removes them from storage.

## Explicit deletes

When explicit deletes are enabled, `in` does not remove objects immediately. Instead it marks the object invisible and returns a `delete_id`: a 32-character hex string generated with a cryptographically secure random source. The caller must confirm deletion by calling `del(delete_id)` within the visibility timeout (default: 30 seconds). If `del` does not arrive in time, the object reappears in the space and becomes eligible for another `in`.

Two invariants hold:

1. No two active `delete_id` values can refer to the same object simultaneously. An object receives a `delete_id` only when selected by `in`, and `in` skips objects with an active (non-expired) visibility timeout.
2. A `delete_id` whose visibility timeout has expired cannot delete anything. `del` checks that the timeout has not passed before deleting.

When explicit deletes are disabled (the default), `in` deletes objects immediately and returns no `delete_id`.

## Blocking

When `in` or `rd` receives a positive `wait`, the operation blocks if no match exists immediately. ACE provides two blocking implementations, chosen in the configuration:

**Polling** queries storage repeatedly with exponential backoff until a match appears or the deadline passes.

**Event-driven notification** registers the caller's pattern with a matching engine. When a new object arrives via `out`, the engine tests it against all active patterns and wakes matching callers. Each caller that wakes re-queries storage to retrieve the object.

A blocked operation terminates when any of these conditions occurs:

1. A matching object appears: the operation returns the object.
2. The `wait` deadline passes: the operation returns nothing.
3. The caller cancels the operation: the operation returns a cancellation error.
4. The space shuts down: the operation returns nothing.

## Limits

These limits constrain every operation. A violation produces an error that states both the observed and maximum values (e.g., "pattern has 6 > 4 leaves").

| Property | Default |
|----------|---------|
| Object size | 2048 bytes |
| Property name size | 64 bytes |
| Object value size | 128 bytes |
| Object leaves | 8 |
| Pattern size | 2048 bytes |
| Pattern leaves | 4 |
| Pattern array length | 4 |
| Pattern atomic value size | 128 bytes |
| Access size | 1024 bytes |
| Access identifiers | 16 |
| TTL maximum | 7 days |
| Caller ID size | 128 bytes |

An object leaf is a scalar value at any nesting depth. A pattern leaf is a single constraint: a scalar is one leaf, and an array of alternatives is one leaf. Metadata properties (names starting with `#`) do not count as leaves.
