# Security

The gateway holds two kinds of secret that matter: upstream provider credentials
and the client API keys that authorise traffic. This document states what
protects each, and what is deliberately out of scope.

## What is stored, and in what form

| Value | At rest | Recoverable? |
|---|---|---|
| Provider credential | AES-256-GCM under `APP_SECRET` (random 96-bit nonce prefixed to the ciphertext) | Yes — the gateway must replay it to the provider. |
| Client API key | HMAC-SHA256 digest under `APP_SECRET`, plus a 12-character non-secret prefix for lookup | **No.** There is no endpoint that returns a key after creation. |
| Operator password | bcrypt, cost 12 | No. |
| Operator session | SHA-256 of a 256-bit random token | No. The token exists only in the client's cookie. |
| Request metadata | alias, key label, provider, status, latency, TTFT, token counts | n/a |
| **Prompt and completion content** | **never written** | n/a |

The `requests` table has no column for content, so a future change cannot start
leaking it without a schema migration that is visible in review.

Rotating `APP_SECRET` makes every stored provider credential unreadable. That is
a deliberate consequence of encryption-at-rest rather than a defect; the recovery
path is to re-enter each credential, and `GET /api/admin/overview` plus the
provider list make it obvious which ones need it.

## Controls

**First-run claiming.** A fresh installation has no operator, so the first-run
endpoint is reachable. It requires `BOOTSTRAP_TOKEN` from the environment and
refuses to run once any operator exists. An installation with the token unset has
setup closed entirely rather than open.

**Sign-in.** Unknown username and wrong password produce the same message and
comparable work (a bcrypt comparison runs either way), so the endpoint cannot be
used to enumerate accounts. Changing a password requires the current one and
signs out every other session.

**Session handling.** The cookie is `HttpOnly` and `SameSite=Lax`. `Secure` is on
unless `INSECURE_COOKIES=true`, which is only acceptable on plain-HTTP localhost.
Sessions are stored server-side and deleted on sign-out, so a stolen cookie is
revocable rather than merely expiring.

**Client keys.** Compared in constant time against the stored digest. A key can
carry a per-minute limit, a concurrency limit, a token quota, an expiry and an
alias allowlist. Revocation is immediate and the row is kept so historical traces
stay attributable.

**Limits fail closed.** The rate-limit counter lives in Redis. If Redis is
unreachable the request is refused with `503 limiter_unavailable`; it is never
served unmetered. Readiness reports `503` in the same condition.

**SSRF.** Provider base URLs are validated before saving and before a connection
test: `http`/`https` only, no embedded credentials, and the cloud metadata range
(`169.254.0.0/16`, `metadata.google.internal`, `100.100.100.200`) is refused
unconditionally. Private and loopback addresses are allowed by default because a
self-hosted gateway normally talks to an upstream on its own network; set
`ALLOW_PRIVATE_UPSTREAMS=false` to refuse them. The dispatch client refuses to
follow redirects, so a credential cannot be forwarded to another host by a
redirect.

**Response handling.** Bounded request (4 MiB) and buffered response (8 MiB)
limits. Upstream error bodies are read up to 4 KiB for the log and never copied
to the client. Only sanitised error codes are persisted.

**Console.** Served same-origin with `default-src 'self'`, `frame-ancestors
'none'`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and
`Referrer-Policy: no-referrer`. Static paths are resolved with `filepath.Join`
and a prefix check, so `..` cannot escape the bundle root.

**Bind default.** The gateway binds loopback unless `ALLOW_PUBLIC_BIND=true`. In
Compose the container binds every interface but the host mapping is
`127.0.0.1:${GATEWAY_PORT}`, so nothing is exposed by default.

## Threat model

| Adversary | Mitigation |
|---|---|
| Someone who finds an unclaimed gateway | Bootstrap token; setup closed once an operator exists. |
| Someone who reads the database | Provider credentials need `APP_SECRET`; client keys are unrecoverable digests; no prompt or completion content is present at all. |
| Someone who intercepts a client key | Keys are revocable and expirable; rotate with a grace window to cut over without downtime. |
| A caller trying to reach internal services | Metadata range refused; no redirect following; private ranges configurable off. |
| A caller trying to exhaust the gateway | Per-key RPM and concurrency limits; bounded bodies; per-attempt timeouts. |
| A malicious console user (XSS) | The console never receives a stored credential; CSP forbids third-party script. |

## Out of scope for this release

- **Multi-tenancy and RBAC.** There is one operator role. Every admin session
  can do everything.
- **TLS termination.** The gateway speaks HTTP; a reverse proxy terminates HTTPS.
  `INSECURE_COOKIES=false` is required once that is in place.
- **Audit immutability.** Audit rows are append-only by convention and there is no
  endpoint that edits them, but a database administrator can. Off-host shipping
  is not implemented.
- **Encryption key rotation without re-entry.** No re-wrap path for stored
  credentials.
- **Distributed rate limiting correctness across replicas.** Concurrency is
  tracked in-process, which is correct for the single-process MVP only.
- **Per-key IP allowlists, request signing, or content filtering.**
