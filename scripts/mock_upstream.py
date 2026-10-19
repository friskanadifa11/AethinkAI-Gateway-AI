#!/usr/bin/env python3
"""Mock OpenAI-compatible upstreams for the end-to-end test.

Three ports, one behaviour each, so a single test run can exercise the healthy
path, a retryable upstream failure, and a terminal authentication failure:

  9091  healthy    answers /models and /chat/completions, streaming or not
  9092  failing    answers /models, then always 500 on /chat/completions
  9093  denied     answers /models, then always 401 on /chat/completions

Nothing here calls a real provider, so `make e2e` costs nothing.
"""

import json
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HEALTHY, FAILING, DENIED = 9091, 9092, 9093
MODEL_PATH = "/v1/chat/completions"
REPLY = "hello from the mock upstream"


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    port = HEALTHY

    # Keep the test output readable.
    def log_message(self, *_args):  # noqa: D401
        return

    def _json(self, status: int, payload: dict) -> None:
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.endswith("/models"):
            self._json(200, {"object": "list", "data": [{"id": "mock-model", "object": "model"}]})
            return
        self._json(404, {"error": {"message": "no such path"}})

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length)
        try:
            payload = json.loads(raw or b"{}")
        except json.JSONDecodeError:
            self._json(400, {"error": {"message": "malformed json"}})
            return

        if not self.path.endswith(MODEL_PATH):
            self._json(404, {"error": {"message": "no such path"}})
            return

        # The gateway always forwards a credential; a mock that accepts anything
        # would not notice a regression that drops the header.
        if not self.headers.get("Authorization", "").startswith("Bearer "):
            self._json(401, {"error": {"message": "missing credential"}})
            return

        if self.port == FAILING:
            self._json(500, {"error": {"message": "upstream is unhappy", "type": "server_error"}})
            return
        if self.port == DENIED:
            self._json(401, {"error": {"message": "bad credential", "type": "auth_error"}})
            return

        if payload.get("stream"):
            self._stream(payload)
        else:
            # A model named *-slow holds the connection open, which is what lets
            # the concurrency ceiling be tested for real instead of assumed.
            if str(payload.get("model", "")).endswith("-slow"):
                time.sleep(1.5)
            self._json(
                200,
                {
                    "id": "chatcmpl-mock",
                    "object": "chat.completion",
                    "created": int(time.time()),
                    "model": payload.get("model", "mock-model"),
                    # Echo the upstream model back so the test can prove the
                    # alias was rewritten before dispatch.
                    "choices": [
                        {
                            "index": 0,
                            "message": {"role": "assistant", "content": REPLY},
                            "finish_reason": "stop",
                        }
                    ],
                    "usage": {"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18},
                },
            )

    def _stream(self, payload: dict) -> None:
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "keep-alive")
        self.end_headers()

        model = payload.get("model", "mock-model")
        for piece in REPLY.split(" "):
            chunk = {
                "id": "chatcmpl-mock",
                "object": "chat.completion.chunk",
                "created": int(time.time()),
                "model": model,
                "choices": [{"index": 0, "delta": {"content": piece + " "}, "finish_reason": None}],
            }
            self._sse(json.dumps(chunk))
            time.sleep(0.01)

        self._sse(
            json.dumps(
                {
                    "id": "chatcmpl-mock",
                    "object": "chat.completion.chunk",
                    "created": int(time.time()),
                    "model": model,
                    "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
                    # Only sent because the gateway asked for usage reporting.
                    "usage": {"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18},
                }
            )
        )
        self._sse("[DONE]")

    def _sse(self, data: str) -> None:
        self.wfile.write(f"data: {data}\n\n".encode())
        self.wfile.flush()


def serve(port: int) -> None:
    handler = type(f"Handler{port}", (Handler,), {"port": port})
    server = ThreadingHTTPServer(("0.0.0.0", port), handler)
    server.serve_forever()


if __name__ == "__main__":
    for p in (HEALTHY, FAILING, DENIED):
        threading.Thread(target=serve, args=(p,), daemon=True).start()
    print("mock upstreams listening on 9091 (healthy), 9092 (failing), 9093 (denied)", flush=True)
    while True:
        time.sleep(3600)
