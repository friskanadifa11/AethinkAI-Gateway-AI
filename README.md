# AethinkAI Gateway AI

An OpenAI-compatible routing gateway: one stable endpoint in front of any number
of upstream providers, with explicit fallback order, per-key limits, and a
request trace for every call.

This is an original implementation written against the AethinkAI Gateway AI PRD.
It does not wrap, proxy or rebrand another dashboard: the Go service, the
PostgreSQL schema, the console and the tests in this repository are all built
here.

```
caller ──► POST /v1/chat/completions  {model: "fast-chat"}
                     │
                     ├─ resolve alias → published route version
                     ├─ walk targets by position, retry then fall back
                     ├─ enforce key limits (Redis + in-process)
                     └─ record one trace per request, one row per attempt
```

## What it does

- **Routing** — an alias is a stable name; each change is a new immutable
  version that must be published. Targets are tried in position order, so a
  fallback is configuration rather than a client concern.
- **Failover that is honest** — a request only fails over if nothing has been
  written to the client yet. A stream that breaks mid-flight is recorded as
  `stream_interrupted` rather than silently retried.
- **Keys that cannot leak** — a client key is stored as an HMAC-SHA256 digest
  plus a short lookup prefix. It is shown once, at creation, and there is no
  endpoint that returns it again.
- **Provider credentials at rest** — AES-256-GCM under `APP_SECRET`, never
  returned through the API, and reported to the console only as
  `credential_set`.
- **Traces without content** — the request log stores metadata only: alias,
  provider, status, latency, time-to-first-token and token counts. No prompt and
  no completion is ever persisted.
- **Limits that fail closed** — an RPM counter in Redis and an in-process
  concurrency counter. If Redis is unreachable the request is refused with
  `503 limiter_unavailable` rather than served unmetered.

## Quickstart

Requires Docker with Compose. Nothing else.

```bash
make bootstrap     # writes .env with freshly generated secrets
make smoke         # builds and starts postgres, redis and the gateway
```

Then open `http://127.0.0.1:8787/app/` and create the first operator using the
`BOOTSTRAP_TOKEN` from `.env`.

Send your first request:

```bash
curl http://127.0.0.1:8787/v1/chat/completions \
  -H "Authorization: Bearer $GATEWAY_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "fast-chat", "messages": [{"role": "user", "content": "Hello"}]}'
```

`make down` stops the stack and keeps your data. `make reset-data` is the only
target that deletes anything, and it refuses to run outside a development `.env`.

## Layout

```text
apps/api/          Go service: public API, admin API, console host
  cmd/api/         entry point
  internal/        config, security, store, gateway
apps/web/          React + TypeScript console
deploy/            Dockerfile.api, compose.yaml
scripts/           mock upstream, end-to-end test
docs/              decisions, security, runbook, contract, test evidence
design-reference/  the routing-first visual reference
```

## Commands

| Command | What it does |
|---|---|
| `make bootstrap` | Generate `.env` with fresh secrets. Refuses to overwrite. |
| `make dev` | Start Postgres and Redis, then run the API from source. |
| `make test` | Go unit tests and the console's Vitest suite. |
| `make lint` | `go vet`, `go build`, and the TypeScript typecheck. |
| `make build` | Production image (console + gateway in one binary image). |
| `make e2e` | Full journey against real Postgres, Redis and mock upstreams. |
| `make smoke` | Start the Compose stack and wait for readiness. |
| `make down` | Stop services, keep volumes. |
| `make reset-data` | **Destructive, local only.** Drop volumes. |

`make e2e` never contacts a paid provider: it runs three mock upstreams (healthy,
failing, denied) and asserts the fallback, the trace and the error paths.

## Configuration

All configuration is environment-based; see `.env.example` for the full list
with comments. The values you must supply yourself:

| Variable | Purpose |
|---|---|
| `APP_SECRET` | Keys credential encryption and key hashing. Keep it stable and back it up. |
| `BOOTSTRAP_TOKEN` | One-use token that authorises creating the first operator. |
| `DATABASE_URL` | PostgreSQL connection string. |
| `REDIS_ADDR` | Redis host:port for rate-limit counters. |

A handful of values change the security posture and are documented as such:
`ALLOW_PUBLIC_BIND` (binding every interface), `INSECURE_COOKIES` (plain-HTTP
cookies), and `ALLOW_PRIVATE_UPSTREAMS` (private/loopback upstream URLs — cloud
metadata addresses are always refused).

## Documentation

- `docs/API-CONTRACT.md` — the public surface, exactly as implemented.
- `docs/SECURITY.md` — threat model and the controls that address it.
- `docs/RUNBOOK.md` — deploy, backup, restore, rotate, troubleshoot.
- `docs/DECISIONS.md` — why the design is what it is.
- `docs/IMPLEMENTATION-STATUS.md` — what is done and what is deliberately not.
- `docs/TEST-EVIDENCE.md` — the commands run and their real output.

## License

See `LICENSE`.
