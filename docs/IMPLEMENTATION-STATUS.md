# Implementation status

Honest inventory of what exists in this repository, and what deliberately does
not. Nothing below is aspirational: every "done" item has a file and, where it
matters, a test that exercises it.

## Done

### Gateway service (`apps/api`)

| Area | State | Where |
|---|---|---|
| OpenAI-compatible `POST /v1/chat/completions`, buffered and streaming | done | `internal/gateway/public_chat.go`, `public_dispatch.go` |
| `GET /v1/models` scoped to the key's allowlist | done | `internal/gateway/public_auth.go` |
| Client-key auth: HMAC digest, constant-time compare, enabled/revoked/expired/quota checks | done | `public_auth.go` |
| Priority routing with per-target retries and failover | done | `routing.go`, `public_dispatch.go` |
| Failover refused after the first byte; interruption recorded | done | `public_dispatch.go` |
| Per-key sliding-window RPM and cross-replica concurrency limits, fail-closed | done | `limits.go` |
| Per-provider circuit breaker: three failures open it for thirty seconds | done | `breaker.go`, `public_dispatch.go` |
| Client-key lifecycle: create, edit limits/aliases/expiry, rotate with grace, revoke | done | `admin_keys.go`, `store/keys.go` |
| Per-key usage rollup over a window, and `key_id` filtering on the request log | done | `store/keys.go`, `admin_observability.go` |
| Request and attempt tracing with no prompt or completion content | done | `public_dispatch.go`, `store/requests.go` |
| Provider credentials encrypted at rest (AES-256-GCM) | done | `internal/security/crypto.go` |
| Versioned routes with an atomic single-published-version guarantee | done | `store/routes.go`, migration index |
| Deterministic routing preview (`dry-run`) with no upstream contact | done | `admin_routes.go` |
| Connection test that calls `/models` and cannot trigger paid inference | done | `admin_providers.go` |
| Operator auth: bootstrap token, bcrypt, server-side sessions | done | `admin_auth.go`, `store/users.go` |
| SSRF validation on provider base URLs | done | `ssrf.go` |
| Audit trail of operator actions | done | `store/users.go`, `admin_observability.go` |
| Retention purge and session cleanup on a timer | done | `cmd/api/main.go` |
| Graceful shutdown with a bounded drain | done | `cmd/api/main.go` |
| Liveness and readiness separated; readiness requires both stores | done | `health.go` |
| Security headers and a console-specific CSP | done | `server.go` |
| Migrations applied transactionally, in order, idempotently | done | `store/store.go`, `migrations/0001_init.sql` |
| Structured JSON logging with probe noise excluded | done | `server.go` |

### Console (`apps/web`)

Nine pages, all backed by real endpoints: Overview, Routing, Providers, Requests,
API keys, Status, Audit log, API reference, Settings. Plus first-run setup and
sign-in. Highlights:

- Design tokens in one file (`src/design/tokens.css`); no page hardcodes a colour.
- One component library (`src/components/ui.tsx`) so pages cannot drift.
- Keyboard and screen-reader support: skip link, `aria-current` on the active
  rail item, focus moved to the page heading on navigation, focus trapping and
  Escape in dialogs, `aria-live` toasts.
- Motion is opt-out: the operator's own preference wins over the OS default, and
  reduced collapses every duration to near zero.
- Honest states throughout: real empty states, real errors with the server's own
  message, and a chart that draws only stored data.
- No charting or routing dependency; React and Vite are the only runtime deps.

### Delivery

| Item | State |
|---|---|
| Docker Compose stack (postgres, redis, gateway), loopback-bound | done |
| Multi-stage image: console build, static binary, minimal runtime, self-probing healthcheck | done |
| `Makefile` implementing the PRD developer contract (`bootstrap` … `reset-data`) | done |
| End-to-end test with mock upstreams, no paid inference | done (`scripts/e2e.py`) |
| CI: format, vet, build, test, typecheck, unit tests, e2e, image build | done (`.github/workflows/ci.yml`) |
| Unit tests: routing rules, SSRF policy, URL normalisation, body rewriting, crypto, password hashing | done |

## Deliberately not done

Each of these is a real gap, not an oversight. They are recorded in
`docs/SECURITY.md` and `docs/DECISIONS.md` with the reasoning.

- **Multi-tenancy and roles.** One operator role; every admin session can do
  everything.
- **TLS.** The gateway speaks HTTP. A reverse proxy terminates TLS.
- **Credential re-wrap.** No way to rotate `APP_SECRET` without re-entering each
  provider credential.
- **Adapters beyond `openai-compatible`.** The adapter and base URL are stored
  columns, so a second one is a data change plus one function.
- **Configurable strategies beyond `priority`.** Sequential fallback only; no
  weighted or cheapest-first selection.
- **CORS on `/v1`.** Browser clients are refused by the same-origin policy; the
  public API is for servers today.
- **Per-key IP allowlists.** Keys are not bound to a source address.
- **Endpoints beyond chat completions.** `/v1/models` is served; embeddings,
  images and audio are not.
- **Metrics and alerting.** No Prometheus/OTLP export; the overview reads the
  request table.
- **Multiple operators.** One operator row: audit entries name the action, not a
  distinct person.
- **Off-host audit shipping.** Audit rows are append-only by convention, not by
  guarantee.
- **Prompt or completion capture.** Absent by design, including for debugging.
- **Structured log forwarding** (OTLP, etc.). Logs go to stdout as JSON.
- **Browser automation in CI.** The e2e test drives the HTTP API; the console is
  built, typechecked and unit tested, but not driven headlessly in CI.
