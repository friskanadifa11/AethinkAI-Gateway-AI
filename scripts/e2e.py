#!/usr/bin/env python3
"""End-to-end test for the AethinkAI Gateway.

Runs the real stack — Postgres, Redis, the Go gateway binary and a set of mock
upstreams — inside Docker, then drives the full operator journey over HTTP:

    health -> bootstrap -> provider -> route -> publish -> key -> call -> trace

Nothing here talks to a paid provider: the upstreams are local mocks, so a full
run costs nothing and is safe to wire into CI.

Usage:  python3 scripts/e2e.py [--keep] [--no-build]
Exit:   0 when every check passes, 1 otherwise.
"""

from __future__ import annotations

import argparse
import http.cookiejar
import json
import os
import pathlib
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
NETWORK = "aethink-e2e-net"
PG, REDIS, MOCK, GW = "aethink-e2e-pg", "aethink-e2e-redis", "aethink-e2e-mock", "aethink-e2e-gw"
PORT = 18787
BASE = f"http://127.0.0.1:{PORT}"
DB_PASSWORD = "e2e-password"
APP_SECRET = "e2e-secret-value-that-is-long-enough-to-pass"
BOOTSTRAP_TOKEN = "e2e-bootstrap-token"
METRICS_TOKEN = "e2e-metrics-token"
ADMIN_USER, ADMIN_PASS = "operator", "correct-horse-battery"

RESULTS: list[tuple[bool, str, str]] = []


def check(name: str, ok: bool, detail: str = "") -> bool:
    RESULTS.append((ok, name, detail))
    mark = "PASS" if ok else "FAIL"
    line = f"  [{mark}] {name}"
    if detail and not ok:
        line += f" — {detail}"
    print(line, flush=True)
    return ok


def sh(*args: str, capture: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(args, capture_output=capture, text=True)


def docker_rm(*names: str) -> None:
    for name in names:
        sh("docker", "rm", "-f", name)


def wait_for(url: str, timeout: float = 60.0, expect_json: bool = True) -> dict | None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=3) as response:
                if response.status == 200:
                    return json.loads(response.read()) if expect_json else {}
        except Exception:
            time.sleep(1)
    return None


class Client:
    """Minimal HTTP client that keeps the operator session cookie."""

    def __init__(self) -> None:
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))

    def call(self, path: str, method: str = "GET", body: dict | None = None,
             headers: dict | None = None, raw: bool = False):
        data = json.dumps(body).encode() if body is not None else None
        request = urllib.request.Request(f"{BASE}{path}", data=data, method=method)
        request.add_header("Accept", "text/event-stream" if raw else "application/json")
        if data:
            request.add_header("Content-Type", "application/json")
        for key, value in (headers or {}).items():
            request.add_header(key, value)
        try:
            with self.opener.open(request, timeout=30) as response:
                payload = response.read()
                return response.status, dict(response.headers), self._decode(payload, raw)
        except urllib.error.HTTPError as error:
            payload = error.read()
            return error.code, dict(error.headers), self._decode(payload, raw)

    @staticmethod
    def _decode(payload: bytes, raw: bool):
        if raw:
            return payload.decode("utf-8", "replace")
        try:
            return json.loads(payload) if payload else None
        except json.JSONDecodeError:
            return payload.decode("utf-8", "replace")


def start_stack(rebuild: bool) -> None:
    print("• tearing down any previous run", flush=True)
    docker_rm(GW, MOCK, REDIS, PG)
    sh("docker", "network", "rm", NETWORK)

    print("• creating network and data services", flush=True)
    sh("docker", "network", "create", NETWORK)
    sh("docker", "run", "-d", "--name", PG, "--network", NETWORK,
       "-e", "POSTGRES_USER=aethink", "-e", f"POSTGRES_PASSWORD={DB_PASSWORD}",
       "-e", "POSTGRES_DB=aethink", "postgres:16-alpine")
    sh("docker", "run", "-d", "--name", REDIS, "--network", NETWORK, "redis:7-alpine")
    sh("docker", "run", "-d", "--name", MOCK, "--network", NETWORK,
       "-v", f"{ROOT / 'scripts'}:/srv:ro", "python:3-alpine", "python", "/srv/mock_upstream.py")

    print("• building the gateway image" if rebuild else "• starting the gateway",
          flush=True)
    if rebuild:
        build = sh("docker", "build", "-f", "deploy/Dockerfile.api", "-t", "aethink-e2e:local", ".")
        if build.returncode != 0:
            print(build.stdout[-2000:], build.stderr[-2000:], flush=True)
            raise SystemExit("image build failed")
        image, command = "aethink-e2e:local", []
    else:
        # Reuse the module cache so repeat runs are seconds rather than minutes.
        image, command = "golang:1.24-alpine", ["go", "run", "./cmd/api"]

    args = [
        "docker", "run", "-d", "--name", GW, "--network", NETWORK,
        "-p", f"127.0.0.1:{PORT}:8080",
        "-e", "HTTP_ADDR=0.0.0.0:8080", "-e", "ALLOW_PUBLIC_BIND=true",
        "-e", f"APP_SECRET={APP_SECRET}", "-e", f"BOOTSTRAP_TOKEN={BOOTSTRAP_TOKEN}",
        "-e", "LOG_LEVEL=warn", "-e", "INSECURE_COOKIES=true",
        "-e", "CORS_ORIGINS=https://console.example",
        "-e", f"METRICS_TOKEN={METRICS_TOKEN}",
        "-e", "DATABASE_URL=postgres://aethink:" + DB_PASSWORD + "@" + PG + ":5432/aethink?sslmode=disable",
        "-e", f"REDIS_ADDR={REDIS}:6379",
    ]
    if command:
        args += ["-v", f"{ROOT / 'apps' / 'api'}:/src", "-v", "/root/work/gocache:/go", "-w", "/src"]
        args += [image, *command]
    else:
        args += [image]
    sh(*args)


def journey(client: Client) -> None:
    # ---------------------------------------------------------------- health
    print("\n• health and readiness", flush=True)
    status, _, body = client.call("/healthz")
    check("liveness answers 200", status == 200, str(body))
    status, _, body = client.call("/readyz")
    check("readiness reports postgres and redis up", status == 200 and body.get("postgres") is True
          and body.get("redis") is True, str(body))

    # ------------------------------------------------------------- bootstrap
    print("\n• first-run setup", flush=True)
    status, _, setup = client.call("/api/admin/setup-state")
    check("a fresh installation needs bootstrap", status == 200 and setup.get("needs_bootstrap") is True,
          str(setup))
    status, _, body = client.call("/api/admin/bootstrap", "POST",
                                  {"username": ADMIN_USER, "password": ADMIN_PASS, "token": "wrong"})
    check("bootstrap refuses a wrong token", status == 403 and body["error"]["code"] == "bad_token",
          str(body))
    status, _, body = client.call("/api/admin/bootstrap", "POST",
                                  {"username": ADMIN_USER, "password": ADMIN_PASS,
                                   "token": BOOTSTRAP_TOKEN})
    check("bootstrap creates the operator", status == 201 and body.get("username") == ADMIN_USER,
          str(body))
    status, _, body = client.call("/api/admin/bootstrap", "POST",
                                  {"username": "second", "password": ADMIN_PASS,
                                   "token": BOOTSTRAP_TOKEN})
    check("bootstrap is closed once an operator exists", status == 409, str(body))
    status, _, body = client.call("/api/admin/me")
    check("the session cookie authenticates /me", status == 200 and body.get("username") == ADMIN_USER,
          str(body))

    # -------------------------------------------------------------- provider
    print("\n• providers", flush=True)
    status, _, primary = client.call("/api/admin/providers", "POST", {
        "name": "mock-healthy", "base_url": "http://aethink-e2e-mock:9091/v1",
        "api_key": "sk-mock-healthy", "adapter": "openai-compatible",
        "timeout_ms": 8000, "enabled": True, "models": ["mock-model"],
    })
    check("a provider can be created", status == 201, str(primary))
    status, _, failing = client.call("/api/admin/providers", "POST", {
        "name": "mock-failing", "base_url": "http://aethink-e2e-mock:9092/v1",
        "api_key": "sk-mock-failing", "adapter": "openai-compatible",
        "timeout_ms": 8000, "enabled": True, "models": ["mock-model"],
    })
    check("a second provider can be created", status == 201, str(failing))
    status, _, body = client.call("/api/admin/providers", "POST", {
        "name": "metadata", "base_url": "http://169.254.169.254/v1", "api_key": "x",
    })
    check("a cloud metadata base URL is refused", status == 400, str(body))
    status, _, body = client.call(f"/api/admin/providers/{primary['id']}/test", "POST")
    check("connection test reports the provider healthy", status == 200 and body.get("state") == "healthy",
          str(body))
    status, _, listed = client.call("/api/admin/providers")
    caps = listed["providers"][0]
    check("a stored credential is never returned",
          "api_key" not in caps and "credential_enc" not in caps, str(caps))

    # ----------------------------------------------------------------- route
    print("\n• routing", flush=True)
    status, _, draft = client.call("/api/admin/routes", "POST", {
        "alias": "fast-chat", "strategy": "priority", "targets": [
            {"provider_id": failing["id"], "upstream_model": "mock-model", "timeout_ms": 5000, "retries": 0},
            {"provider_id": primary["id"], "upstream_model": "mock-model", "timeout_ms": 5000, "retries": 1},
        ],
    })
    check("a route version is created as a draft", status == 201 and draft.get("published") is False,
          str(draft))
    status, _, body = client.call("/api/admin/routes", "POST", {
        "alias": "fast-chat", "strategy": "priority", "targets": [
            {"provider_id": primary["id"], "upstream_model": "mock-model"},
            {"provider_id": primary["id"], "upstream_model": "mock-model"},
        ],
    })
    check("a duplicate target is refused", status == 400, str(body))
    status, _, plan = client.call(f"/api/admin/routes/{draft['id']}/dry-run", "POST")
    check("dry run lists both hops in order without contacting a provider",
          status == 200 and len(plan["would_use"]) == 2
          and plan["would_use"][0]["provider_name"] == "mock-failing",
          str(plan))
    status, _, published = client.call(f"/api/admin/routes/{draft['id']}/publish", "POST")
    check("the version can be published", status == 200 and published.get("published") is True,
          str(published))

    # ------------------------------------------------------------------- key
    print("\n• client keys", flush=True)
    status, _, minted = client.call("/api/admin/keys", "POST", {
        "label": "e2e-runner", "owner_label": "ci", "rpm": 0, "max_concurrency": 0,
        "token_quota": 0, "expires_in_days": 0, "allowed_aliases": [],
    })
    key = minted.get("token", "")
    check("a key is issued exactly once", status == 201 and key.startswith("atk_"), str(minted)[:200])
    status, _, listed = client.call("/api/admin/keys")
    check("the key list exposes only the prefix",
          all("token" not in k and k["prefix"] == key[:12] for k in listed["keys"]))

    key_id = minted["key"]["id"]
    status, _, body = client.call(f"/api/admin/keys/{key_id}?range=1h")
    check("a key reports its own usage", status == 200 and "usage" in body and body["usage"]["requests"] == 0,
          str(body)[:200])

    status, _, body = client.call(f"/api/admin/keys/{key_id}", "PATCH", {"rpm": 42})
    check("a key is edited in place instead of rotated",
          status == 200 and body["key"]["rpm"] == 42 and body["key"]["prefix"] == key[:12],
          str(body)[:200])

    status, _, body = client.call(f"/api/admin/keys/{key_id}", "PATCH",
                                  {"allowed_aliases": ["never-published"]})
    check("an unpublished alias is refused when editing a key",
          status == 400 and body["error"]["code"] == "unknown_alias", str(body))

    status, _, body = client.call("/api/admin/keys", "POST", {
        "label": "typo-key", "owner_label": "", "rpm": 0, "max_concurrency": 0,
        "token_quota": 0, "expires_in_days": 0, "allowed_aliases": ["fast-chta"],
    })
    check("a mistyped alias is refused at creation rather than issuing a dead key",
          status == 400 and body["error"]["code"] == "unknown_alias", str(body))

    # ------------------------------------------------------------- public API
    print("\n• public API", flush=True)
    public = Client()
    auth = {"Authorization": f"Bearer {key}"}
    status, _, body = public.call("/v1/models", headers=auth)
    check("/v1/models lists the published alias",
          status == 200 and any(m["id"] == "fast-chat" for m in body["data"]), str(body))

    status, _, body = public.call("/v1/chat/completions", "POST",
                                  {"model": "fast-chat", "messages": [{"role": "user", "content": "hi"}]},
                                  headers=auth)
    content = body["choices"][0]["message"]["content"] if status == 200 else ""
    check("a request succeeds through fallback",
          status == 200 and "mock upstream" in content, str(body)[:300])

    status, _, body = public.call("/v1/chat/completions", "POST",
                                  {"model": "fast-chat", "messages": [{"role": "user", "content": "hi"}],
                                   "stream": True}, headers=auth, raw=True)
    check("a streaming request returns SSE with a usage chunk",
          status == 200 and '"usage"' in body and "[DONE]" in body, body[:200])

    # ---------------------------------------------------------------- limits
    print("\n• limits", flush=True)
    status, _, limited = client.call("/api/admin/keys", "POST", {
        "label": "burst-key", "owner_label": "ci", "rpm": 2, "max_concurrency": 0,
        "token_quota": 0, "expires_in_days": 0, "allowed_aliases": ["fast-chat"],
    })
    burst = {"Authorization": f"Bearer {limited['token']}"}
    codes = []
    for _ in range(3):
        status, _, body = public.call(
            "/v1/chat/completions", "POST",
            {"model": "fast-chat", "messages": [{"role": "user", "content": "burst"}]},
            headers=burst)
        codes.append(status)
    check("the per-minute window admits exactly its budget then refuses",
          codes == [200, 200, 429], str(codes))

    status, _, slow = client.call("/api/admin/routes", "POST", {
        "alias": "slow-chat", "strategy": "priority", "targets": [
            {"provider_id": primary["id"], "upstream_model": "reply-slow", "timeout_ms": 10000},
        ],
    })
    client.call(f"/api/admin/routes/{slow['id']}/publish", "POST")
    status, _, conc = client.call("/api/admin/keys", "POST", {
        "label": "conc-key", "owner_label": "ci", "rpm": 0, "max_concurrency": 1,
        "token_quota": 0, "expires_in_days": 0, "allowed_aliases": ["slow-chat"],
    })
    conc_auth = {"Authorization": f"Bearer {conc['token']}"}
    outcomes: list[int] = []
    lock = threading.Lock()

    def fire() -> None:
        # Each thread gets its own opener: urllib's is not safe to share.
        worker = Client()
        code, _, _ = worker.call(
            "/v1/chat/completions", "POST",
            {"model": "slow-chat", "messages": [{"role": "user", "content": "hold"}]},
            headers=conc_auth)
        with lock:
            outcomes.append(code)

    threads = [threading.Thread(target=fire) for _ in range(2)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    check("the concurrency ceiling is enforced, not just documented",
          sorted(outcomes) == [200, 429], str(outcomes))

    # ------------------------------------------------------- provider health
    print("\n• provider health", flush=True)
    # fast-chat's primary hop answers 500 every time. Three failures in a row
    # should take it out of rotation, so a caller stops paying that timeout on
    # every request.
    for _ in range(2):
        public.call("/v1/chat/completions", "POST",
                    {"model": "fast-chat", "messages": [{"role": "user", "content": "burn"}]},
                    headers=auth)
    time.sleep(0.6)
    status, _, fleet = client.call("/api/admin/providers")
    failing_row = next(p for p in fleet["providers"] if p["name"] == "mock-failing")
    check("a provider that keeps failing is shown as cooling down",
          failing_row.get("health") == "open" and failing_row.get("cooldown_s", 0) > 0,
          str(failing_row))

    status, headers, body = public.call(
        "/v1/chat/completions", "POST",
        {"model": "fast-chat", "messages": [{"role": "user", "content": "after"}]},
        headers=auth)
    trace = headers.get("X-Aethink-Trace", "")
    _, _, detail = client.call(f"/api/admin/requests/{trace}")
    first = (detail.get("attempts") or [{}])[0]
    check("the next request skips the open provider instead of paying its timeout",
          status == 200 and first.get("status") == "skipped"
          and first.get("error_code") == "circuit_open", str(detail)[:300])

    status, _, body = public.call("/v1/chat/completions", "POST",
                                  {"model": "fast-chat", "messages": []})
    check("a missing key is rejected", status == 401 and body["error"]["code"] == "missing_api_key",
          str(body))
    status, _, body = public.call("/v1/chat/completions", "POST",
                                  {"model": "fast-chat", "messages": []},
                                  headers={"Authorization": "Bearer atk_not_a_real_key"})
    check("an unknown key is rejected", status == 401 and body["error"]["code"] == "invalid_api_key",
          str(body))
    status, _, body = public.call("/v1/chat/completions", "POST",
                                  {"model": "no-such-alias", "messages": []}, headers=auth)
    check("an unknown alias answers model_not_found",
          status == 404 and body["error"]["code"] == "model_not_found", str(body))

    # -------------------------------------------------- per-key model scoping
    # A customer key must reach only the aliases it was issued for. This is the
    # whole point of issuing one key per customer, so it is checked as behaviour
    # rather than trusted because the column exists.
    print("\n• per-key model scoping", flush=True)
    status, _, scoped = client.call("/api/admin/routes", "POST", {
        "alias": "premium-chat", "strategy": "priority", "targets": [
            {"provider_id": primary["id"], "upstream_model": "mock-model-premium",
             "timeout_ms": 5000, "retries": 0},
        ],
    })
    check("a second alias is created", status == 201, str(scoped)[:200])
    status, _, body = client.call(f"/api/admin/routes/{scoped['id']}/publish", "POST")
    check("the second alias is published", status == 200 and body.get("published") is True,
          str(body)[:200])

    status, _, restricted = client.call("/api/admin/keys", "POST", {
        "label": "customer-a", "owner_label": "customer-a", "rpm": 0, "max_concurrency": 0,
        "token_quota": 0, "expires_in_days": 0, "allowed_aliases": ["fast-chat"],
    })
    customer = {"Authorization": f"Bearer {restricted.get('token', '')}"}
    check("a key can be limited to one alias", status == 201, str(restricted)[:200])

    status, _, body = public.call("/v1/models", headers=customer)
    ids = sorted(m["id"] for m in body["data"])
    check("a limited key only lists its own alias", status == 200 and ids == ["fast-chat"], str(ids))

    status, _, body = public.call("/v1/chat/completions", "POST",
                                  {"model": "fast-chat", "messages": [{"role": "user", "content": "hi"}]},
                                  headers=customer)
    check("a limited key can call its own alias", status == 200, str(body)[:200])

    status, _, body = public.call("/v1/chat/completions", "POST",
                                  {"model": "premium-chat", "messages": [{"role": "user", "content": "hi"}]},
                                  headers=customer)
    check("a limited key cannot call another alias",
          status == 404 and body["error"]["code"] == "model_not_found", str(body))

    # The refusal must look exactly like a model that never existed, or a
    # customer can map the catalogue by watching which errors come back.
    status, _, unknown = public.call("/v1/chat/completions", "POST",
                                     {"model": "never-existed", "messages": []}, headers=customer)
    same_shape = (status == 404 and unknown["error"]["code"] == body["error"]["code"]
                  and unknown["error"]["type"] == body["error"]["type"])
    check("a forbidden alias is indistinguishable from a missing one", same_shape,
          str(unknown) + " vs " + str(body))

    status, _, body = public.call("/v1/models", headers=auth)
    ids_all = sorted(m["id"] for m in body["data"])
    _, _, catalogue = client.call("/api/admin/routes")
    published_now = sorted(r["alias"] for r in catalogue["published"])
    check("an unrestricted key still lists every published alias",
          ids_all == published_now and "fast-chat" in published_now, str(ids_all))

    # ------------------------------------------------------- cors and metrics
    print("\n• cors and metrics", flush=True)
    status, headers, _ = public.call("/v1/chat/completions", "OPTIONS",
                                     headers={"Origin": "https://console.example",
                                              "Access-Control-Request-Method": "POST"})
    check("a listed origin passes the preflight",
          status == 204 and headers.get("Access-Control-Allow-Origin") == "https://console.example",
          str({k: v for k, v in headers.items() if k.lower().startswith("access-control")}))

    status, headers, _ = public.call("/v1/chat/completions", "OPTIONS",
                                     headers={"Origin": "https://evil.example"})
    check("an unlisted origin is granted nothing",
          status == 204 and "Access-Control-Allow-Origin" not in headers, str(headers))

    status, headers, _ = public.call("/v1/models",
                                     headers={**auth, "Origin": "https://console.example"})
    check("a real answer carries the grant and exposes the trace header",
          status == 200 and headers.get("Access-Control-Allow-Origin") == "https://console.example"
          and "X-Aethink-Trace" in headers.get("Access-Control-Expose-Headers", ""), str(headers))

    status, _, body = public.call("/metrics")
    check("metrics are closed without the token", status == 401, str(body)[:120])
    status, _, body = public.call("/metrics", headers={"Authorization": f"Bearer {METRICS_TOKEN}"})
    text = body if isinstance(body, str) else json.dumps(body)
    check("metrics expose real counts once authorised",
          status == 200 and 'aethink_requests_total{alias="fast-chat",status="ok"}' in text
          and 'aethink_limit_refusals_total{reason="rate"}' in text, text[:300])

    # ------------------------------------------------------------ observability
    print("\n• observability", flush=True)
    status, _, log = client.call("/api/admin/requests?since=30m&limit=50")
    check("the request log contains the calls just made", status == 200 and log["total"] >= 4,
          str(log.get("total")))
    failover = next((r for r in log["requests"]
                     if r["status"] == "ok" and not r["streamed"] and r["fallback_count"] >= 1), None)
    check("the successful call recorded a fallback", failover is not None and failover["fallback_count"] >= 1,
          str(failover))
    if failover:
        status, _, detail = client.call(f"/api/admin/requests/{failover['trace_id']}")
        codes = [a.get("error_code", "") for a in detail["attempts"]]
        check("the trace shows the failing attempt then the good one",
              status == 200 and len(detail["attempts"]) == 2 and codes[0] == "upstream_error"
              and codes[1] == "",
              str(detail))
    status, _, overview = client.call("/api/admin/overview?range=1h")
    check("overview aggregates real traffic",
          status == 200 and overview["requests"] >= 4 and overview["errors"] >= 2
          and len(overview["series"]) >= 1, str(overview)[:300])
    check("overview counts the provider fleet", len(overview["providers"]) == 2,
          str(len(overview["providers"])))

    # -------------------------------------------------------------- lifecycle
    print("\n• key lifecycle and audit", flush=True)
    key_id = listed["keys"][0]["id"]
    status, _, revoked = client.call(f"/api/admin/keys/{key_id}/revoke", "POST")
    check("a key can be revoked", status == 204, str(revoked))
    status, _, body = public.call("/v1/chat/completions", "POST",
                                  {"model": "fast-chat", "messages": []}, headers=auth)
    check("a revoked key is refused", status == 401 and body["error"]["code"] == "key_revoked",
          str(body))
    status, _, events = client.call("/api/admin/audit?limit=50")
    actions = {e["action"] for e in events["events"]}
    check("operator actions were audited",
          status == 200 and {"operator.bootstrap", "route.published", "key.created", "key.revoked"} <= actions,
          str(sorted(actions)))

    status, _, body = client.call("/api/admin/settings", "PUT", {"site_name": "E2E Gateway"})
    check("settings can be updated", status == 200 and body["site_name"] == "E2E Gateway", str(body))
    status, _, body = client.call("/api/admin/settings", "PUT", {"timezone": "Not/AZone"})
    check("an invalid timezone is refused", status == 400, str(body))

    status, _, body = client.call("/api/admin/logout", "POST")
    check("sign-out succeeds", status == 204, str(body))
    status, _, body = client.call("/api/admin/overview")
    check("the session no longer works", status == 401, str(body))


def main() -> int:
    parser = argparse.ArgumentParser(description="AethinkAI Gateway end-to-end test")
    parser.add_argument("--keep", action="store_true", help="leave containers running for inspection")
    parser.add_argument("--no-build", action="store_true",
                        help="run the Go source with `go run` instead of building the image")
    args = parser.parse_args()

    if sh("docker", "version").returncode != 0:
        print("docker is required to run the end-to-end test", file=sys.stderr)
        return 2

    start_stack(rebuild=not args.no_build)

    print("• waiting for readiness", flush=True)
    ready = wait_for(f"{BASE}/readyz", timeout=180)
    if ready is None:
        # Say why, not just that it failed: a readiness timeout with no logs is
        # the least useful failure this script can produce.
        print("\n• the gateway never became ready; evidence follows", file=sys.stderr)
        for name, tail in ((GW, 60), (PG, 20), (REDIS, 10)):
            logs = sh("docker", "logs", "--tail", str(tail), name)
            stream = ((logs.stdout or "") + (logs.stderr or "")).strip()
            print(f"--- {name} ---\n{stream or '(no output)'}", file=sys.stderr)
            state = sh("docker", "inspect", "-f",
                       "status={{.State.Status}} exit={{.State.ExitCode}}", name)
            print(f"({state.stdout.strip()})", file=sys.stderr)
        if args.keep:
            print(f"containers left running: {GW} {PG} {REDIS} {MOCK}", file=sys.stderr)
        else:
            docker_rm(GW, MOCK, REDIS, PG)
        return 1

    client = Client()
    try:
        journey(client)
    finally:
        if args.keep:
            print(f"\ncontainers left running; gateway on {BASE}", flush=True)
        else:
            docker_rm(GW, MOCK, REDIS, PG)
            sh("docker", "network", "rm", NETWORK)

    passed = sum(1 for ok, _, _ in RESULTS if ok)
    failed = [name for ok, name, _ in RESULTS if not ok]
    print(f"\n{passed}/{len(RESULTS)} checks passed")
    if failed:
        print("failed: " + "; ".join(failed))
        return 1
    print("end-to-end run OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
