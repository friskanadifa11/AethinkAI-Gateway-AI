// Status: the only page that is useful without signing in, because "is the
// gateway up?" must be answerable when the console session has expired.
//
// Both probes are read live from the gateway; nothing is cached or invented.

import { useCallback, useEffect, useState } from "react";
import { Badge, Button, Panel, Spinner } from "../components/ui";
import { ms } from "../lib/format";

type Probe = {
  path: string;
  label: string;
  detail: string;
  status: number | null;
  latencyMs: number | null;
  body: Record<string, unknown> | null;
  checkedAt: string;
};

async function probe(path: string, label: string, detail: string): Promise<Probe> {
  const started = performance.now();
  try {
    const response = await fetch(path, { credentials: "same-origin" });
    const latencyMs = performance.now() - started;
    let body: Record<string, unknown> | null = null;
    try {
      body = (await response.json()) as Record<string, unknown>;
    } catch {
      body = null;
    }
    return { path, label, detail, status: response.status, latencyMs, body, checkedAt: new Date().toISOString() };
  } catch {
    return { path, label, detail, status: null, latencyMs: null, body: null, checkedAt: new Date().toISOString() };
  }
}

export function StatusPage() {
  const [probes, setProbes] = useState<Probe[]>([]);
  const [checking, setChecking] = useState(false);

  const run = useCallback(async () => {
    setChecking(true);
    const results = await Promise.all([
      probe("/healthz", "Liveness", "The process is running and serving requests."),
      probe("/readyz", "Readiness", "Postgres and Redis both answer, so limits and tracing work."),
    ]);
    setProbes(results);
    setChecking(false);
  }, []);

  useEffect(() => {
    void run();
    const timer = window.setInterval(() => void run(), 15000);
    return () => window.clearInterval(timer);
  }, [run]);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-prose text-xs muted">
          Checked every 15 seconds while this page is open. Readiness is the one that matters for
          traffic: the gateway refuses requests rather than serve them without rate limiting.
        </p>
        <Button size="sm" onClick={() => void run()} busy={checking}>
          Check now
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {probes.map((p) => {
          const healthy = p.status === 200;
          return (
            <Panel
              key={p.path}
              title={p.label}
              subtitle={p.detail}
              actions={
                <Badge tone={p.status === null ? "bad" : healthy ? "ok" : "warn"} pulse={healthy}>
                  {p.status === null ? "unreachable" : healthy ? "healthy" : `status ${p.status}`}
                </Badge>
              }
            >
              <dl className="flex flex-col gap-2 text-xs">
                <div className="flex justify-between gap-4">
                  <dt className="muted">Endpoint</dt>
                  <dd className="ident">{p.path}</dd>
                </div>
                <div className="flex justify-between gap-4">
                  <dt className="muted">Latency</dt>
                  <dd className="ident">{p.latencyMs === null ? "—" : ms(p.latencyMs)}</dd>
                </div>
                <div className="flex justify-between gap-4">
                  <dt className="muted">Checked</dt>
                  <dd className="ident">{p.checkedAt.slice(11, 19)}Z</dd>
                </div>
                {p.body && (
                  <div className="mt-1 rounded-[var(--radius-sm)] border border-[color:var(--surface-border)] bg-[color:var(--surface-void)] px-3 py-2">
                    <pre className="ident whitespace-pre-wrap break-all">
                      {JSON.stringify(p.body, null, 2)}
                    </pre>
                  </div>
                )}
              </dl>
            </Panel>
          );
        })}
        {probes.length === 0 && (
          <Panel title="Checking">
            <span className="flex items-center gap-2 text-xs muted">
              <Spinner /> Probing the gateway…
            </span>
          </Panel>
        )}
      </div>
    </div>
  );
}
