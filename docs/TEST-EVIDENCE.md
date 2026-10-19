# Test evidence

Every result below is real output from this repository, produced on the build
machine. Commands are copy-pasteable; where a container is used, that is stated,
because Go and Node are not installed on the host.

No test contacts a paid provider. The end-to-end run uses three local mocks.

## 1. Gateway: format, vet, build, unit tests

```
$ docker run --rm -v "$PWD/apps/api:/src" -w /src golang:1.24-alpine \
    sh -c "gofmt -l . && go vet ./... && go build ./... && go test ./..."
(no output from gofmt -l → every file is gofmt-clean)
(no output from go vet or go build → clean)

?   .../internal/config      [no test files]
ok  .../internal/gateway      0.006s
?   .../internal/models      [no test files]
ok  .../internal/security     0.855s
?   .../internal/store       [no test files]
```

Covered by those tests: target ordering and timeout precedence, dropping of
disabled/missing targets, the empty-target case, status classification and
retryability, SSRF acceptance and refusal (including the cloud metadata range),
base-URL normalisation across `/v1` and `/v1beta`, `RewriteModel` field
preservation, `stream_options` injection, usage extraction, the alias policy,
AES-GCM round-trip, wrong-secret and tamper rejection, non-deterministic
ciphertext, key shape and hashing, constant-time comparison, bcrypt, and token
uniqueness.

## 2. Console: typecheck, unit tests, production build

```
$ docker run --rm -v "$PWD/apps/web:/w" -w /w node:22-alpine sh -c \
    "npx tsc --noEmit && npx vitest run && npx vite build"
tsc: no errors

 ✓ src/components/ui.modal.test.tsx (3 tests) 528ms
 ✓ src/lib/format.test.ts (16 tests) 9ms
 Test Files  2 passed (2)
      Tests  19 passed (19)

vite v5.4.21 building for production...
✓ 46 modules transformed.
dist/index.html                   0.99 kB │ gzip:  0.52 kB
dist/assets/index-NyqrnpQC.css   18.39 kB │ gzip:  4.75 kB
dist/assets/index-Cyve3Uo5.js   215.06 kB │ gzip: 65.17 kB
✓ built in 2.95s
```

The console's unit tests cover the formatting helpers as behaviour contracts
(compacting counts, switching to seconds above 1s, guarding division by zero,
relative timestamps, never rendering a full key, and status-to-tone mapping),
and the dialog focus trap.

`ui.modal.test.tsx` is a regression test for a defect that made every form in
the console unfillable. The dialog's focus effect listed `onClose` as a
dependency, but callers build a fresh closure on every render, so each keystroke
tore the effect down and re-ran it, and the re-run focused the dialog's first
focusable element. Typing one character moved the caret onto a button. The test
asserts where focus is after typing, not merely that the value changed: a
controlled input passes a value-only check while still being unusable.

Against the unfixed component the test fails with
`expected <button …> to be <input …>`, which is the defect reproduced rather
than described.

## 3. Production image

```
$ docker build -f deploy/Dockerfile.api -t aethinkai/gateway:local .
#20 [web build] RUN npm run build   → ✓ built in 6.41s
#21 [build]  RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" …
#23 [stage-2] COPY --from=web /web/dist /app/web
#24 exporting to image → naming to docker.io/aethinkai/gateway:local
```

Three stages: the console is built with Node 22, the gateway is compiled to a
static binary, and the runtime is Alpine with the binary and the built console.
The image is its own health probe (`-healthcheck`), so the runtime needs no
`curl`.

## 4. End-to-end journey

```
$ python3 scripts/e2e.py --no-build
```

Real Postgres 16, Redis 7, the gateway compiled from source, and three mock
upstreams (healthy on 9091, always-500 on 9092, always-401 on 9093):

```
• health and readiness
  [PASS] liveness answers 200
  [PASS] readiness reports postgres and redis up
• first-run setup
  [PASS] a fresh installation needs bootstrap
  [PASS] bootstrap refuses a wrong token
  [PASS] bootstrap creates the operator
  [PASS] bootstrap is closed once an operator exists
  [PASS] the session cookie authenticates /me
• providers
  [PASS] a provider can be created
  [PASS] a second provider can be created
  [PASS] a cloud metadata base URL is refused
  [PASS] connection test reports the provider healthy
  [PASS] a stored credential is never returned
• routing
  [PASS] a route version is created as a draft
  [PASS] a duplicate target is refused
  [PASS] dry run lists both hops in order without contacting a provider
  [PASS] the version can be published
• client keys
  [PASS] a key is issued exactly once
  [PASS] the key list exposes only the prefix
  [PASS] a key reports its own usage
  [PASS] a key is edited in place instead of rotated
  [PASS] an unpublished alias is refused when editing a key
  [PASS] a mistyped alias is refused at creation rather than issuing a dead key
• public API
  [PASS] /v1/models lists the published alias
  [PASS] a request succeeds through fallback
  [PASS] a streaming request returns SSE with a usage chunk
• limits
  [PASS] the per-minute window admits exactly its budget then refuses
  [PASS] the concurrency ceiling is enforced, not just documented
• provider health
  [PASS] a provider that keeps failing is shown as cooling down
  [PASS] the next request skips the open provider instead of paying its timeout
  [PASS] a missing key is rejected
  [PASS] an unknown key is rejected
  [PASS] an unknown alias answers model_not_found
• per-key model scoping
  [PASS] a second alias is created
  [PASS] the second alias is published
  [PASS] a key can be limited to one alias
  [PASS] a limited key only lists its own alias
  [PASS] a limited key can call its own alias
  [PASS] a limited key cannot call another alias
  [PASS] a forbidden alias is indistinguishable from a missing one
  [PASS] an unrestricted key still lists every published alias
• cors and metrics
  [PASS] a listed origin passes the preflight
  [PASS] an unlisted origin is granted nothing
  [PASS] a real answer carries the grant and exposes the trace header
  [PASS] metrics are closed without the token
  [PASS] metrics expose real counts once authorised
• observability
  [PASS] the request log contains the calls just made
  [PASS] the successful call recorded a fallback
  [PASS] the trace shows the failing attempt then the good one
  [PASS] overview aggregates real traffic
  [PASS] overview counts the provider fleet
• key lifecycle and audit
  [PASS] a key can be revoked
  [PASS] a revoked key is refused
  [PASS] operator actions were audited
  [PASS] settings can be updated
  [PASS] an invalid timezone is refused
  [PASS] sign-out succeeds
  [PASS] the session no longer works

57/57 checks passed
end-to-end run OK
```
```

The checks that carry the routing and tenancy claims:

- A request whose primary target answers 500 still answers `200` from the
  fallback, and its trace holds **two attempts in order** — `upstream_error`,
  then success.
- The same failing provider, after three failures, is listed as `health: "open"`
  with a cooldown, and the next request's trace records `skipped` /
  `circuit_open` instead of paying that failure again.
- A key budgeted at two requests per minute gets `200, 200, 429` — and a key
  capped at one in-flight request gets exactly one `200` and one `429` out of two
  simultaneous calls.
- A key issued for one alias sees only that alias in `/v1/models`, and calling
  another one answers `404 model_not_found` — indistinguishable from an alias
  that does not exist.
- A listed CORS origin gets its grant on the preflight and on the real response,
  with `X-Aethink-Trace` exposed; an unlisted one gets nothing.
- `/metrics` refuses a bare request and returns per-alias counters once the
  bearer token is supplied.

## 5. Compose stack

```
$ docker compose --env-file .env -f deploy/compose.yaml up -d --build
 Container aethinkai-gateway-postgres-1 Healthy
 Container aethinkai-gateway-redis-1    Healthy
 Container aethinkai-gateway-gateway-api-1 Started

$ curl -sS http://127.0.0.1:8787/readyz
{"postgres":true,"redis":true,"status":"ready"}

$ curl -sS http://127.0.0.1:8787/healthz
{"status":"ok"}

$ docker compose … ps
aethinkai-gateway-gateway-api-1   Up   127.0.0.1:8787->8080/tcp
aethinkai-gateway-postgres-1      Up (healthy)
aethinkai-gateway-redis-1         Up (healthy)
```

Console delivery, checked directly against the running stack:

```
$ curl -sS http://127.0.0.1:8787/app/                     → 200 text/html
$ curl -sS http://127.0.0.1:8787/app/assets/index-*.js    → 200 text/javascript  215172 bytes
$ curl -sS http://127.0.0.1:8787/app/assets/index-*.css   → 200 text/css          18390 bytes
$ curl -sS -o /dev/null -w '%{http_code}' …/app/routing   → 200 (SPA deep link)
$ curl -sS -o /dev/null -w '%{http_code}' …/app/../../etc/passwd → 404 (traversal refused)
```

## 6. Console in a real browser

Driven with the browser tool against the Compose stack, using the session the API
issued (no password typed into any form, no stored credential read):

```
=== rail navigation ===
links: 9
  Overview         href=/app/overview  aria-current=page
  Routing          href=/app/routing   aria-current=None
  Providers        href=/app/providers aria-current=None
  Requests         href=/app/requests  aria-current=None
  API keys         href=/app/keys      aria-current=None
  Status           href=/app/status    aria-current=None
  Audit log        href=/app/audit     aria-current=None
  API reference    href=/app/docs      aria-current=None
  Settings         href=/app/settings  aria-current=None

=== accessibility scaffolding ===
skip link: Skip to content
main landmark: True
aside landmark: True

=== page render audit (case-insensitive) ===
  OK  overview   OK  routing    OK  providers   OK  keys
  OK  requests   OK  audit      OK  status      OK  docs   OK  settings
  (no page threw and no console error was logged on any of them)

=== overflow sweep ===
  1440x900: overflow=0
  1280x800: overflow=0
   768x1024: overflow=0
   390x844: overflow=0   (the compact rail scrolls inside its own container)
```

### Dependency readiness, verified rather than assumed

The gateway used to exit on the first refused connection, so a Postgres that was
still running its first `initdb` killed the process. The end-to-end script caught
this as a bare readiness timeout, which is what prompted the change.

The fix (bounded retry with backoff) was verified by starting the gateway alone
on a network with no Postgres and no Redis at all:

```
$ docker run -d --name aethink-demo-gw ... aethink-e2e:local
$ sleep 7 && docker inspect -f '{{.State.Status}}' aethink-demo-gw
running                      # not exited: it waits

{"level":"WARN","msg":"waiting for postgres and redis","attempt":3,"retry_in":"1s",
 "error":"ping postgres: ... hostname resolving error: lookup aethink-demo-pg ..."}
... attempts 4, 5, 6, 7 ...

$ docker run -d --name aethink-demo-pg ... postgres:16-alpine   # start it late
$ curl -sS http://127.0.0.1:18788/readyz
{"postgres":true,"redis":true,"status":"ready"}

waiting for postgres and redis (x7) → dependencies reachable → migration applied
→ gateway listening
```

A readiness failure in the end-to-end script now prints each container's logs and
exit status instead of nothing, because a silent timeout was the reason this took
longer to find than it should have.

Honesty of the empty state, verified on an installation with no providers and no
traffic: the Overview renders `No providers configured`, `No traffic in this
window` and `No failures recorded`, and the metric tiles read zero. Nothing is
filled with sample data. Once one operator exists, the Audit page shows the real
`operator.bootstrap` row rather than the empty state.

## Reproducing

```bash
make test     # gateway + console unit tests
make lint     # gofmt, go vet, go build, tsc
make build    # production image
make e2e      # the 44-check journey, mock upstreams only
make smoke    # Compose stack + readiness
```
