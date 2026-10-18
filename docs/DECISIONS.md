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

## D4 — Redis counter, in-process concurrency

**Decision.** RPM lives in Redis; the in-flight count lives in the process.

**Why.** A restart must not reset a caller's per-minute budget, so that counter
belongs in shared state. A concurrency count has no meaning across a network hop
— an in-flight request belongs to one process — and Redis round-trips on the hot
path would cost latency for no correctness gain. The honest consequence is
recorded in SECURITY.md: with more than one replica, concurrency is per-replica.

## D5 — A single `openai-compatible` adapter

**Decision.** One adapter. Adapter and base URL are stored columns, not a plugin
registry.

**Why.** The public contract is OpenAI's; every provider worth routing to speaks
it. A registry would be abstraction without a second implementation. The columns
exist so a second adapter is a data change plus one function, not a redesign.

## D6 — Fixed-window rate limiting

**Decision.** A per-minute bucket keyed on `(key, minute)`.

**Why.** A sliding window needs either sorted sets or a token bucket, both of
which cost more per request and are harder to explain. The fixed window permits a
burst at a minute boundary — a caller can spend two minutes' budget in two
adjacent windows — and that is an acceptable trade for a single-operator
gateway where the limit exists to stop runaway loops, not to shape traffic.

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
