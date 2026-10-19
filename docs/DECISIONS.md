# Decisions

Short records of the choices that are not obvious from the code. Each one states
the alternative that was rejected and why.

## D1 — Routing is versioned, not edited in place

**Decision.** An alias owns one immutable published version. Editing means
creating a new version and publishing it.

**Why.** A live route that can be edited in place means a mis-click changes
production traffic with no review and no rollback. With versions, "what was
serving at 14:02?" has an answer, and rollback is publishing the previous version
again. A partial unique index (`routes_one_published_per_alias`) makes the
publish atomic against concurrent writers, so it cannot produce two live versions.

## D2 — Failover only before the first byte

**Decision.** A target is retried, and the next target consulted, only if nothing
has been written to the client yet. A stream that breaks mid-flight is recorded
as `stream_interrupted`.

**Why.** Replaying a half-delivered stream would duplicate tokens the caller
already saw and was already billed for. Reporting the interruption is the honest
option; silently retrying would look like success while producing a corrupt
response.

## D3 — Client keys are hashed, and there is no reveal endpoint

**Decision.** Store HMAC-SHA256 plus a 12-character lookup prefix. Show the key
once, at creation.

**Why.** An HMAC with a server secret means a database dump alone does not yield
usable keys, and the prefix keeps lookup O(1) instead of scanning every row with
a constant-time compare. A "reveal" feature would make the whole scheme
pointless, so instead there is a rotate endpoint with a grace window: the
operational need is a cutover without downtime, not the ability to read an old
secret.

## D4 — Both limit counters live in Redis

**Decision.** The request window and the in-flight count are both Redis counters.

**Why.** A restart must not reset a caller's per-minute budget, so that counter
belongs in shared state. The in-flight count was originally per-process, on the
argument that a request belongs to one process — true, but the *limit* belongs to
the key, so behind three replicas a ceiling of four became twelve. The counter is
a plain `INCR`/`DECR` with a five-minute TTL: the TTL is not the limit, it is the
repair, so slots held by a replica that dies mid-request disappear instead of
pinning the key at its ceiling forever. Release uses a detached context because
the request that acquired the slot may already be cancelled.

## D5 — A single `openai-compatible` adapter

**Decision.** One adapter. Adapter and base URL are stored columns, not a plugin
registry.

**Why.** The public contract is OpenAI's; every provider worth routing to speaks
it. A registry would be abstraction without a second implementation. The columns
exist so a second adapter is a data change plus one function, not a redesign.

## D6 — Sliding-window rate limiting

**Decision.** A sorted set of request timestamps per key, counted over the true
last sixty seconds.

**Why.** The fixed `(key, minute)` bucket allowed twice the configured rate at a
boundary: the whole budget in the last second of one minute, then the whole budget
again in the first second of the next. That is not a theoretical edge — a retry
storm lands exactly there. The sorted set costs one pipeline of four commands and
is trimmed on every request, so it never grows past the budget itself. A refused
request is removed from the window again, because it must not consume budget it
never used.

## D7 — No charting or routing library in the console

**Decision.** Hand-rolled SVG chart and a ~60-line path router. React and Vite
are the only runtime dependencies.

**Why.** Nine fixed destinations do not justify a router that pulls in history
handling, and one bar chart does not justify a charting library. Fewer
dependencies means a smaller supply-chain surface for a component that holds
operator credentials, and the chart is more accessible this way: the bars carry
the real numbers in `title` text rather than hiding them in canvas.

## D8 — Traces are detached from the request context

**Decision.** The trace row is written on a background context with a 5-second
timeout.

**Why.** A caller that hangs up mid-stream would otherwise cancel the write, and
the requests most worth investigating are exactly the ones callers abandon. The
cost is that a trace can arrive a moment after the response; that is acceptable
and the 5-second bound keeps it from leaking goroutines.

## D9 — The repository is not a rebrand

**Decision.** Original implementation: Go service, schema, console and tests
written for this repository.

**Why.** The PRD (§0.1–0.2) forbids importing, wrapping, proxying or cosmetically
rebranding another dashboard. A wrapper would also inherit an upstream we do not
control on the exact code path that handles provider credentials. The visual
reference informs the design language (dark navy, single cyan signal accent,
routing depicted as converging lanes); it is not copied.

## D10 — Testing without paid inference

**Decision.** `make e2e` runs three mock upstreams — healthy, failing, denied —
and asserts failover, tracing and error classification against them.

**Why.** A test that needs a real provider key cannot run in CI, costs money, and
is non-deterministic. Mocks make the interesting paths (failover, retry, terminal
4xx) deliberately reproducible, which is what the routing logic actually needs to
be tested against.

## D11 — A provider that keeps failing is taken out of rotation

**Decision.** Three consecutive failed attempts open a per-provider circuit in
Redis for thirty seconds. While open, the target is skipped with
`circuit_open` instead of being attempted.

**Why.** A dead primary stayed first in every route, so every caller paid its full
timeout before reaching the healthy hop. A provider that is down is not
information worth paying for on each request. Redis rather than memory, because
the verdict is about the provider, not the process: one replica discovering the
outage should spare the others. The failure counter survives the cooldown, so the
first probe after it re-opens the breaker on a single failure rather than needing
three more — that keeps recovery cheap for a provider that is genuinely still
down. The state is surfaced on the provider list, because a silent skip is
indistinguishable from a slow gateway when you are looking at a dashboard.

## D12 — Keys are edited in place; the secret is not

**Decision.** `PATCH /api/admin/keys/{id}` changes limits, expiry, aliases and
the `enabled` flag. Omitted fields keep their stored value. The secret has no
update path at all.

**Why.** Rotating a key to fix a typo in its label invalidated a customer's
credential for no reason. The secret is different: only its HMAC is stored, so
there is nothing to edit — a new secret is a rotation, with its grace window and
its own audit entry. Fields are pointers on the wire so `0` (unlimited) stays
distinguishable from "not sent", and `allowed_aliases` is a console picker rather
than a text box, because an unknown alias on a fail-closed allowlist produced a
key that could reach nothing and reported nothing. Creation now rejects unknown
aliases for the same reason.

