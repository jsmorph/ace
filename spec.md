A service for agent coordination inspired by tuple spaces

## Introduction

This service is inspired by tuple spaces.

We generalize "tuple" to value representable in JSON.

What's described here should be considered Version 1. The APIs will be
versioned.

## Specification

A pattern also has the form of a value representable in JSON.  For the
first version, these patterns will be very limited.  (The inspiration
is AWS Event Bridge pattern matching, including the Quamina
implementation). A value matches a pattern if

```
For every branch B with leaf L in the pattern:

  If L is atomic:
    B appears in the object.
  If L is an array [X1,...,Xn]
    B with a leaf Xi appears in the object for some i.
```

Examples:

| pattern                   | object                    | matches? |
|---------------------------|---------------------------|----------|
| {"a":1}                   | {"a":1}                   | yes      |
| {"a":[1,2]}               | {"a":1}                   | yes      |
| {"a":[1,2]}               | {"a":2}                   | yes      |
| {"a":[1,2]}               | {"a":3}                   | no       |
| {"a":[1,2]}               | {"a":1,"b":0}             | yes      |
| {"a":[1,2]}               | {"a":2,"b":0}             | yes      |
| {"a":[1,2]}               | {"a":3,"b":0}             | no       |
| {"b":[1,2]}               | {"a":1}                   | no       |
| {"b":[1,2]}               | {"a":2}                   | no       |
| {"b":[1,2]}               | {"a":3,"b":3}             | no       |
| {"b":[1,2]}               | {"a":3,"b":1}             | yes      |
| {"b":[1,2]}               | {"b":1}                   | yes      |
| {"a":{"b":1,"c":2}}       | {"a":{"b":1,"c":2}}       | yes      |
| {"a":{"b":1,"c":2}}       | {"a":{"b":1,"c":2,"d":3}} | yes      |
| {"a":{"b":1,"c":2},"d":3} | {"a":{"b":1,"c":2,"d":3}} | no       |


For now at least, both values and patterns must be "objects" (as
opposed to atomic values), and we'll say "object" to mean "value".

We implement variants of the common tuple space operations.

1.  `out(object,access,ttl)`: Put the object into the space with the
    given access control, which has this form
    `{"in":[IDS...],"rd":[IDS...]}`, where each `ID` is opaque for
    now.  Both `in` and `rd` properties are optional, and `access`
    itself is optional.  The object exists in the space for `ttl` (in
    ISO duration syntax), which defaults to 72 hours (`P3D`).

1.  `in(pattern,wait,since)`: Get and remove the earliest (see below)
    matching object.  If necessary, block for up to `wait` seconds for
    a matching object.  If `wait` is zero, do not block.  The context
    for this operation includes an `ID` for the caller.  An returned
    object must have that identifier in the object's `access.in` if
    the object has any `assess.in`. `since` is an optional timestamp
    (`YYYY-MM-DDTHH:MM:SS.SSSSSSSSS`). If `since` is provided, only
    objects with a timestamp (see below) greater than `since` are
    considered.

1.  `rd(pattern,wait,since)`: Get (but do not remove) the earliest
    (see below) matching object.  If necessary, block for up to `wait`
    seconds for a matching object.  If `wait` is zero, do not block.
    The context for this operation includes an `ID` for the caller.
    An returned object must have that identifier in the object's
    `access.rd` if the object has any `assess.rd`. `since` is an
    optional timestamp (`YYYY-MM-DDTHH:MM:SS.SSSSSSSSS`). If `since`
    is provided, only objects with a timestamp (see below) greater
    than `since` are considered.

When an object is put into the space, the object gets a timestamp,
which is unique an serves as an identifier.  The timestamp has this
form:

```
YYYY-MM-DDTHH:MM:SS.SSSSSSSSS
```

The implicit time zone is `Z`.

## Implementation

This implementation is in Go, and it uses a pure Go version of SQLite
for persistence.

When an object is written to the space, a JSON serialization of the
object is also writen to the SQL table `objects` as `ID,JSON,EXPIRES`.
`EXPIRES` is based on the `ttl` (with its default).  `ID` is generated
such that it will be unique (see below).  If `access` was given, the
following records are written to the SQL table `access`: `ID,in":IID`
for each `IID` in `access.in` and `ID,"out",OID` for each `OID` in
`access.out`.  The `id` column is a foreign key for `objects.id`.

The `in` and `rd` operations are supported with a index implemented
with a SQL table `branches` and SQL index.  When an object is written
to the space, a `branches` record `ID,B` is written for every branch
`B` in the object. The `id` column is a foreign key for `objects.id`.

For a `out()` call, all of these SQL operations should execute in a
single transaction (but only after non-database, non-side-effect work
like identifier generation, JSON serialization, etc. has already been
computed).

An object `ID` is generated like this: Get the current time at
nanosecond resolution. See if that time is greater than the last
generated identifier.  If it is, store the new identifier as the last
one generated and return it.  Otherwise increment the candidate
identifer, store it, and emit it. This entire operation should be
atomic and in-memory only (no SQL). (We can optimize a little later if
necessary.)

To implement `rd` and `in`, construct and execute a SQL query that
references `objects`, `branches`, and `access` based on the given
pattern.

These limits are checked and enforced.  When a limit is hit, a clear,
concise error message is generated.  "The pattern has 6 > 4 leaves."

The code is structured as a library and a command-line program `ace`,
which has a few subcommands:

1.  `serve --port INTERFACE:PORT --limits FILENAME`: Runs an HTTP
    server with endpoints `/out`, `/in`, `/rd`, and `/limits`.  When
    required, the client's `ID` is at header `X-ACE-ID`.  The port
    defaults to `localhost:8000`.  The endpoint `limits` returns a
    JSON representation of the current user-facing service limits (see
    below).  If `--limits FILENAME`, which is optional, is given, the
    read the service limits (JSON) from the given filename.
   
1. `out --object OBJECT --access ACCESS --ttl TTL`: Do that operation.
   `OBJECT` and `ACCESS` (default) are JSON. TTL (optional) is an ISO
   duration.

1. `in --pattern PATTERN --since TIMESTAMP`: Just do that operation.
   `TIMESTAMP` is optional and has a default of zero time.
   
1. `rd --pattern PATTERN --since TIMESTAMP`: See `in`.

All subcommands also accept `--db FILENAME`, which defaults to
`ace.db`.
   
   

## Limits

| Property                   | Limit      |
|----------------------------|------------|
| Object size                | 2048 bytes |
| Property size              | 64 bytes   |
| Object value size          | 128 bytes  |
| Object leaves              | 8          |
| Pattern size               | 2048 bytes |
| Pattern leaves             | 4          |
| Pattern leaf array length  | 4          |
| Pattern atomic leaf length | 128 bytes  |
| `access` size              | 1024 bytes |
| `access` length            | 16         |
| `ttl` maximum              | 7 days     |
| `ID` size                  | 128 bytes  |


## Next

- [ ] Provide an alternative blocking read implementation (while still
      supporting the existing one with a switch like
      `BlockingImplementation="polling"`. When a client request (`in`
      or `rd`) arrives with `wait` greater than zero, the client
      registers its pattern.  When an `out` is processed, all
      registered clients are checked to see if their patterns match
      the new object.  This work happens after and outside of the
      database transaction, of course.  Clients are notified with the
      matching object (if any). We do this matching (to determine who
      gets notified) synchronously in the ingest processing to provide
      backpressure.  Use the Go package Quamina to do this matching:
      Each client adds its pattern to a single Quamina instance.  The
      value is used to notify that client.  When a client goes away,
      its entry is removed from the Quamina instance.

- [ ] Scavenge objects based on expiration. At every
      `ScavengeInterval` (default 1 hour), server deletes expired
      objects.
	  
- [ ] New CLI subcommand `stats`: Look at the database and report (in
      JSON) important statisics like number of objects, number of
      `branch` records, number of `access` records, average branch
      length, average number of branches per object, average number of
      `access.in` and `access.rd` entries per object.
	  
- [ ] Make sure JSON stored in the database is canonicalized first.

- [ ] Add a parameter for the HTTP service: Max concurrent clients.
      When more than that many clients are waiting (blocking), return
      the appropriate HTTP status (too busy or whatever) for new
      clients (that might block).

- [ ] Write a serious system-level test in Go that is available as a
      subcommand (not a Go test) `test`.  This process does a lot of
      concurrent reads and writes, checking each.  For example, write
      something with an incremented value, and make sure the reads are
      getting the right output.  Focus on possible races and other
      bugs due to concurrency.  Structure this test with parameters to
      control the number of virtual clients, number of requests, the
      polling implementation, etc.  These parameters should be exposed
      on the command line.  This test should use the HTTP API.
	  
## Roadmap

 For later. Do *not* pursue now.

 - [ ] Throttling.

 - [ ] Server-sent events to deliver multiple objects asynchronously
	   for `in` and `rd` operations if requested.

 - [ ] Explicit deletes: Like SQS, an `in` should be followed by a
	   `del(ID)` (new operations) within a "visibility timeout".  If a
	   `del(ID)` is not received in that interval, the object
	   automatically reappears in the space.

