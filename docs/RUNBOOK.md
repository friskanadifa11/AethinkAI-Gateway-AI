# Runbook

## First deploy

```bash
git clone <this repository> && cd Aethink-AI-Gateway
make bootstrap      # generates .env with fresh secrets; refuses to overwrite
make smoke          # builds images, starts postgres + redis + gateway
```

Open `http://127.0.0.1:8787/app/`. The console detects that no operator exists
and shows the first-run form. Username, a password of at least 12 characters,
and the `BOOTSTRAP_TOKEN` value from `.env`.

Once the operator exists the bootstrap endpoint closes permanently. If the token
is unset, first-run setup is closed instead of open — set `BOOTSTRAP_TOKEN` and
restart if you need to claim an installation that was never claimed.

Then, in the console:

1. **Providers** → add an upstream (name, base URL, API key) and press **Test**.
   The check calls `/models`, so it verifies reachability and the credential
   without spending tokens.
2. **Routing** → create a version for an alias, add targets in fallback order,
   **Save draft**, then **Publish**. Use **Preview** to see the exact attempt
   order before publishing.
3. **API keys** → create a key. Copy it immediately; it is not recoverable.

Verify end to end:

```bash
curl http://127.0.0.1:8787/v1/chat/completions \
  -H "Authorization: Bearer $GATEWAY_KEY" -H "Content-Type: application/json" \
  -d '{"model":"fast-chat","messages":[{"role":"user","content":"ping"}]}'
```

## Everyday operations

```bash
make down           # stop services, keep data
make smoke          # start again
docker compose --env-file .env -f deploy/compose.yaml logs -f gateway-api
```

The gateway binds `127.0.0.1` by default. To expose it, put a TLS reverse proxy
in front, set `INSECURE_COOKIES=false`, and only then change the Compose port
mapping. Setting `ALLOW_PUBLIC_BIND=true` is required for the container to bind
every interface; the host mapping is what keeps it private.

## Backup and restore

Only PostgreSQL holds durable state. Redis holds rate-limit counters that are
safe to lose.

```bash
# Backup
docker compose --env-file .env -f deploy/compose.yaml exec -T postgres \
  pg_dump -U aethink -d aethink --format=custom > aethink-$(date +%F).dump

# Encrypt before it leaves the host
age -r <recipient> -o aethink-$(date +%F).dump.age aethink-$(date +%F).dump
```

```bash
# Restore into a fresh instance
make down
docker compose --env-file .env -f deploy/compose.yaml up -d postgres
docker compose --env-file .env -f deploy/compose.yaml exec -T postgres \
  pg_restore -U aethink -d aethink --clean --if-exists < aethink-2026-01-01.dump
docker compose --env-file .env -f deploy/compose.yaml up -d
```

After a restore, sign in and confirm: the Overview shows your traffic, the
provider list shows `credential_set` for every provider, and one provider passes
**Test**. If `credential_set` is false everywhere, the `APP_SECRET` in `.env` is
not the one that encrypted them.

**Back up `APP_SECRET` separately from the database.** A database dump without it
restores routes and keys but no usable provider credentials.

## Rotating a client key

Console → **API keys** → **Rotate**. Enter a grace window in hours (24 by
default). The replacement inherits the original's limits; the old key keeps
working until the window closes, so the caller can switch over without downtime.
The old secret is never displayed again.

## Rotating `APP_SECRET`

This is deliberately disruptive: every stored provider credential becomes
unreadable.

1. Take a backup of the database first.
2. Change `APP_SECRET` and restart.
3. Open **Providers**. Each provider now shows `credential_set` but **Test**
   reports that the credential cannot be decrypted.
4. Re-enter the API key for each one (leave the field blank to keep a working
   credential untouched; only typing a value replaces it).

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `/readyz` returns `503` with `postgres:false` | Postgres is unreachable. Check `DATABASE_URL` and that the container is healthy. |
| `/readyz` returns `503` with `redis:false` | Redis is unreachable. Traffic will be refused with `503 limiter_unavailable` rather than served unmetered. |
| Every request answers `503 no_healthy_target` | The alias's targets are all disabled, or their providers are missing/disabled. Use **Routing → Preview** to see which and why. |
| Every request answers `502 upstream_auth` | The provider rejected the credential. Re-enter it on the Providers page. |
| A provider's **Test** says the credential cannot be decrypted | `APP_SECRET` changed. See "Rotating APP_SECRET". |
| Console shows `console_unbuilt` | `WEB_DIST` points at a directory without `index.html`. The production image sets it to `/app/web`. |
| Sign-in loops back to the login form | The cookie is `Secure` but the site is plain HTTP. Set `INSECURE_COOKIES=true` for local work only. |
| `make bootstrap` refuses to run | A `.env` already exists. That is intentional; move it aside if you really mean to regenerate. |
| Requests appear but with 0 tokens | The provider does not report usage. The gateway records what the provider sends rather than estimating. |

## Logs and tracing

```bash
docker compose --env-file .env -f deploy/compose.yaml logs --tail 200 gateway-api
```

Structured JSON on stdout, one line per request with method, path, status and
duration. Health probes are excluded from the log to keep it readable.

For a single request, the `X-Aethink-Trace` response header names the row to look
up under **Requests**, where every attempt — provider, model, status, latency —
is listed in the order the router tried them.

## Upgrade

```bash
git pull
make build
docker compose --env-file .env -f deploy/compose.yaml up -d
```

Migrations run automatically at startup, in filename order, each inside a
transaction and recorded in `schema_migrations`. A partially applied migration is
rolled back rather than left half-applied. There is no automatic destructive
migration: nothing drops or recreates a table containing data.
