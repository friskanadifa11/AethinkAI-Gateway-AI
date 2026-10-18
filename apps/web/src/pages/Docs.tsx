// API reference. The content here mirrors the gateway's actual behaviour, which
// is deliberate: a reference that drifts from the implementation is worse than
// no reference, so every example below is exercised by scripts/e2e.sh.

import { useState, type ReactNode } from "react";
import { Button, Panel, cx } from "../components/ui";

type Section = { id: string; title: string; body: ReactNode };

const codeBlock =
  "ident block overflow-x-auto whitespace-pre rounded-[var(--radius-sm)] border border-[color:var(--surface-border)] bg-[color:var(--surface-void)] px-3 py-2.5";

function Endpoint({ method, path }: { method: string; path: string }) {
  return (
    <p className="mb-3 flex items-center gap-2">
      <span
        className={cx(
          "rounded px-2 py-0.5 text-[11px] font-semibold",
          method === "POST"
            ? "bg-[color:var(--signal-dim)] text-[color:var(--signal)]"
            : "bg-[color:var(--ok-dim)] text-[color:var(--ok)]",
        )}
      >
        {method}
      </span>
      <span className="ident text-[13px] text-[color:var(--text-strong)]">{path}</span>
    </p>
  );
}

const SECTIONS: Section[] = [
  {
    id: "auth",
    title: "Authentication",
    body: (
      <>
        <p className="mb-3 text-xs muted">
          Every public call carries a client key as a Bearer token. Keys are created in the console
          under API keys and are shown exactly once.
        </p>
        <pre className={codeBlock}>Authorization: Bearer atk_…</pre>
        <p className="mt-3 text-xs muted">
          A missing header answers <span className="ident">401 missing_api_key</span>; an unknown or
          revoked key answers <span className="ident">401 invalid_api_key</span> or{" "}
          <span className="ident">401 key_revoked</span>. An alias the key is not allowed to use is
          reported as <span className="ident">404 model_not_found</span> rather than a 403, so a key
          cannot enumerate aliases it has no access to.
        </p>
      </>
    ),
  },
  {
    id: "chat",
    title: "Chat completions",
    body: (
      <>
        <Endpoint method="POST" path="/v1/chat/completions" />
        <p className="mb-3 text-xs muted">
          The request body is OpenAI-shaped and is forwarded with the{" "}
          <span className="ident">model</span> field rewritten to the alias&apos;s upstream model.
          Every other field is passed through untouched, including ones this gateway does not model.
        </p>
        <pre className={codeBlock}>{`curl http://127.0.0.1:8787/v1/chat/completions \\
  -H "Authorization: Bearer $GATEWAY_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "fast-chat",
    "messages": [{"role": "user", "content": "Say hello in five words."}],
    "max_tokens": 40
  }'`}</pre>
        <p className="mt-3 text-xs muted">
          Streaming works the same way with <span className="ident">&quot;stream&quot;: true</span>.
          The gateway adds <span className="ident">stream_options.include_usage</span> when the
          caller did not set it, so the token accounting on the Requests page is the provider&apos;s
          own number rather than a local estimate.
        </p>
      </>
    ),
  },
  {
    id: "models",
    title: "List aliases",
    body: (
      <>
        <Endpoint method="GET" path="/v1/models" />
        <p className="mb-3 text-xs muted">
          Returns only the published aliases this key is allowed to call, in OpenAI&apos;s list shape,
          so an SDK&apos;s model picker works without modification.
        </p>
        <pre className={codeBlock}>{`curl http://127.0.0.1:8787/v1/models \\
  -H "Authorization: Bearer $GATEWAY_KEY"`}</pre>
      </>
    ),
  },
  {
    id: "routing",
    title: "How a request is routed",
    body: (
      <>
        <ol className="flex flex-col gap-2 text-xs muted">
          <li>
            <span className="text-[color:var(--text-body)]">1.</span> The alias is resolved to its
            single published version.
          </li>
          <li>
            <span className="text-[color:var(--text-body)]">2.</span> Targets are ordered by
            position. A disabled target, or one whose provider is missing or disabled, is skipped.
          </li>
          <li>
            <span className="text-[color:var(--text-body)]">3.</span> A target is tried up to{" "}
            <span className="ident">1 + retries</span> times, but only on a retryable status:
            408, 429 or 5xx.
          </li>
          <li>
            <span className="text-[color:var(--text-body)]">4.</span> A 4xx other than 408 is
            terminal for that target — repeating it would spend money for the same answer.
          </li>
          <li>
            <span className="text-[color:var(--text-body)]">5.</span> The next target is tried only
            if the failure happened before any bytes were sent. Once a stream has started, the
            response cannot be replayed and an interruption is recorded as{" "}
            <span className="ident">stream_interrupted</span>.
          </li>
        </ol>
      </>
    ),
  },
  {
    id: "errors",
    title: "Errors",
    body: (
      <>
        <p className="mb-3 text-xs muted">
          Errors use the OpenAI envelope, so an SDK surfaces the reason without special handling.
          Upstream error bodies are never copied through — only a bounded snippet is read for the
          operator&apos;s log.
        </p>
        <pre className={codeBlock}>{`{
  "error": {
    "message": "Every provider target for fast-chat failed (upstream_rate_limited).",
    "type": "api_error",
    "code": "upstream_rate_limited"
  }
}`}</pre>
        <p className="mt-3 text-xs muted">
          Limit breaches answer <span className="ident">429 rate_limited</span> or{" "}
          <span className="ident">429 concurrency_limited</span> with a{" "}
          <span className="ident">Retry-After</span> header. If the rate limiter itself is
          unreachable the request is refused with{" "}
          <span className="ident">503 limiter_unavailable</span> rather than served unmetered.
        </p>
      </>
    ),
  },
];

export function DocsPage() {
  const [open, setOpen] = useState<string>(SECTIONS[0]!.id);

  return (
    <div className="flex flex-col gap-4">
      <p className="max-w-prose text-xs muted">
        The public contract is deliberately small: two endpoints, one auth scheme, one error shape.
        Anything an operator configures — providers, fallback order, limits — changes the behaviour
        of these calls without changing their shape.
      </p>

      <div className="flex flex-col gap-3">
        {SECTIONS.map((section) => {
          const expanded = open === section.id;
          return (
            <Panel
              key={section.id}
              title={section.title}
              actions={
                <Button size="sm" variant="quiet" onClick={() => setOpen(expanded ? "" : section.id)}>
                  {expanded ? "Collapse" : "Expand"}
                </Button>
              }
            >
              {expanded ? (
                section.body
              ) : (
                <p className="text-xs faint">Collapsed — expand to read this section.</p>
              )}
            </Panel>
          );
        })}
      </div>
    </div>
  );
}
