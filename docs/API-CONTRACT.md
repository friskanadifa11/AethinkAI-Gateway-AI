# API contract

Two surfaces: the public OpenAI-compatible API, which callers use, and the admin
API, which the console uses. This document describes what is implemented — not
what is planned.

All responses are JSON with `Content-Type: application/json; charset=utf-8`.

## Public API

Authentication is a Bearer client key on every endpoint:

```
Authorization: Bearer atk_…
```

### `POST /v1/chat/completions`

The body is OpenAI-shaped. The gateway rewrites only the `model` field, replacing
the alias with the resolved upstream model; every other field — including fields
this gateway does not model, such as `response_format` or `tools` — is forwarded
verbatim.

```json
{
  "model": "fast-chat",
  "messages": [{ "role": "user", "content": "Hello" }],
  "stream": false
}
```

| Response | Meaning |
|---|---|
| `200` with `application/json` | A provider answered. |
| `200` with `text/event-stream` | Streaming. The gateway adds `stream_options.include_usage` when the caller did not set it, so the final chunk carries the provider's own token counts. |
| `4xx` / `5xx` | An OpenAI-shaped error envelope (below). |

Successful non-streaming responses carry two optional headers:

- `X-Aethink-Trace` — the trace id, matching the Requests page.
- `X-Aethink-Fallback: true` — present only when an earlier target failed before
  this one answered.

### `GET /v1/models`

Returns the published aliases this key may call, in OpenAI's list shape:

```json
{ "object": "list", "data": [{ "id": "fast-chat", "object": "model", "owned_by": "aethinkai-gateway" }] }
```

An alias the key is not allowlisted for is simply absent.

## Error envelope

```json
{ "error": { "message": "…", "type": "invalid_request_error", "code": "…" } }
```

| Status | `code` | When |
|---|---|---|
| 400 | `invalid_json` | The body is not a JSON object. |
| 400 | `missing_model` | No `model` field. |
| 400 | `body_too_large` | Body over 4 MiB. |
| 401 | `missing_api_key` | No `Authorization` header. |
| 401 | `invalid_api_key` | Unknown key. |
| 401 | `key_revoked` | Revoked or disabled. |
| 401 | `key_expired` | Past `expires_at`. |
| 404 | `model_not_found` | No published route for that alias, **or** the key is not allowed to use it. The two cases are indistinguishable on purpose: a key must not be able to enumerate aliases it cannot use. |
| 429 | `rate_limited` | Per-minute request limit; `Retry-After: 60`. |
| 429 | `concurrency_limited` | In-flight limit; `Retry-After: 5`. |
| 429 | `quota_exhausted` | Token quota consumed. |
| 429 | `upstream_rate_limited` | Every target answered 429. |
| 502 | `upstream_error` | Every target answered 5xx. |
| 502 | `upstream_auth` | Every target rejected the credential. |
| 503 | `no_healthy_target` | The route has no enabled, existing provider target. |
| 503 | `limiter_unavailable` | Redis is unreachable. The request is refused rather than served unmetered. |
| 504 | `upstream_timeout` | Every target timed out. |

Upstream response bodies are never copied into the client response; at most a
bounded snippet is read for the operator's log, and only sanitised codes are
stored on the trace.

## Routing behaviour

1. The alias resolves to its single published version.
2. Targets are ordered by `position` ascending. A target is dropped when it is
   disabled, or when its provider is missing or disabled.
3. Each target is tried up to `1 + retries` times, but only on a retryable
   status: `408`, `429`, or any `5xx`. Any other `4xx` is terminal for that
   target.
4. The next target is consulted only if nothing was written to the client yet.
5. Once a stream has started, a failure cannot be replayed; the trace records
   `stream_interrupted` and the client sees a truncated stream.

## Admin API

Session-cookie authenticated (`aethink_session`, `HttpOnly`, `SameSite=Lax`).
Unauthenticated endpoints are `GET /api/admin/setup-state`,
`POST /api/admin/bootstrap`, and `POST /api/admin/login`.

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/admin/setup-state` | Whether the installation still needs its first operator. |
| `POST` | `/api/admin/bootstrap` | Requires `BOOTSTRAP_TOKEN`; only while no operator exists. |
| `POST` | `/api/admin/login` | Identical failure message for unknown user and wrong password. |
| `POST` | `/api/admin/logout` | Deletes the session server-side. |
| `GET` | `/api/admin/me` | Current operator. |
| `POST` | `/api/admin/password` | Requires the current password; signs out other sessions. |
| `GET` | `/api/admin/overview` | `?range=1h\|24h\|7d\|30d`. |
| `GET` `POST` | `/api/admin/providers` | List / create. A listed provider carries `health: "open"` and `cooldown_s` while the circuit breaker is skipping it. |
| `PATCH` `DELETE` | `/api/admin/providers/{id}` | Update / remove. An empty `api_key` keeps the stored credential. |
| `POST` | `/api/admin/providers/{id}/test` | Bounded check. Calls `/models`, never a completion: testing costs nothing. |
| `GET` `POST` | `/api/admin/routes` | List versions / create a draft. |
| `POST` | `/api/admin/routes/{id}/publish` | Atomically promotes one version per alias. |
| `POST` | `/api/admin/routes/{id}/dry-run` | Computes the attempt order from stored state. No provider is contacted. |
| `DELETE` | `/api/admin/routes/{id}` | Refuses to delete the live version. |
| `GET` `POST` | `/api/admin/keys` | List / create. Create returns the plaintext key once. |
| `GET` | `/api/admin/keys/{id}` | One key plus its usage rollup (`?range=1h\|24h\|7d\|30d`). |
| `PATCH` | `/api/admin/keys/{id}` | Partial update of label, owner, limits, expiry, aliases, `enabled`. Omitted fields keep their value; an unknown alias is refused with `unknown_alias`. The secret cannot be changed — that is what rotate is for. |
| `POST` | `/api/admin/keys/{id}/rotate` | Issues a replacement and expires the old one after a grace window. |
| `POST` | `/api/admin/keys/{id}/revoke` | Immediate. |
| `GET` | `/api/admin/requests` | `since`, `status`, `alias`, `q`, `limit` (max 200), `offset`. |
| `GET` | `/api/admin/requests/{traceId}` | The request plus its ordered attempts. |
| `GET` `PUT` | `/api/admin/settings` | Partial updates: only the fields present are written. |
| `GET` | `/api/admin/audit` | `?limit=` (max 200). |

Unknown JSON fields are rejected with `400`, so a typo in the console surfaces as
an error rather than a silently ignored field.

## Operational endpoints

| Method | Path | Meaning |
|---|---|---|
| `GET` | `/healthz` | Liveness. Answers while the process runs. |
| `GET` | `/readyz` | Readiness. `503` unless Postgres **and** Redis answer. |
| `GET` | `/metrics` | Prometheus text exposition. Requires `METRICS_TOKEN` (Bearer header or `?token=`); answers `404` when no token is configured. |
| `GET` | `/app/` | The console. Unknown paths under `/app/` fall back to `index.html`. |
