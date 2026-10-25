---
name: performance-review
description: Performance review of a change at the sizes it must handle - algorithmic complexity, database query patterns, memory, blocking I/O, missing limits and timeouts, concurrency, and frontend rendering and bundle size. Use as the performance-reviewer when a change touches hot paths, data access, large inputs or concurrency.
user-invocable: false
---

# Performance review

Follow the `review-protocol` skill for scope, findings and output. Your `id`
prefix is `PERF`.

## Start from the sizes

Take the sizes from the spec's Constraints: rows, requests per second,
payload sizes, list lengths, concurrent users. If the spec names none, state
the sizes you assume and say so in `not_reviewed`. A finding must say at what
size it starts to matter, with the arithmetic: "N+1 over 500 orders is 501
queries, about 2 s at 4 ms each".

## What to look for

- **Complexity**: nested loops over data that grows, repeated linear searches
  that want a map, sorting inside a loop.
- **Data access**: N+1 queries, queries without an index for their filter or
  order, `SELECT *` of large rows, missing pagination, loading a whole table
  to count or filter it, transactions held across network calls.
- **Memory**: reading whole files or responses into memory, unbounded caches
  and queues, slices or arrays grown in a loop without capacity.
- **I/O and blocking**: synchronous I/O on Node's event loop, blocking calls
  in async Python, sequential calls that could run concurrently, chatty
  network round-trips.
- **Limits**: outbound calls without timeouts, retries without backoff or a
  cap, unbounded concurrency (goroutines or promises per item).
- **Leaks**: goroutines, timers, listeners or connections never released.
- **Frontend**: re-renders from unstable props or context values, large
  lists without virtualisation, heavy dependencies in the main bundle, work in
  render that belongs in a memo or the server.

## What not to report

Micro-optimisations without a measured or estimated effect at the stated
sizes, and style preferences. If you suspect a problem but cannot estimate
it, say what measurement would settle it (a benchmark, a query plan) instead
of guessing a severity.
