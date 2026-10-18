// Overview: the page an operator opens first. Every number comes from stored
// requests — an installation with no traffic shows zeros and an explanation,
// never sample data.

import { useCallback, useEffect, useState } from "react";
import { api, type Overview as OverviewData } from "../lib/api";
import { compact, grouped, ms, pct, relative, tone } from "../lib/format";
import { Badge, Cell, EmptyState, ErrorNote, Loading, Metric, Panel, Row, Table, cx } from "../components/ui";

const RANGES = ["1h", "24h", "7d", "30d"] as const;

export function OverviewPage({ onOpenTrace }: { onOpenTrace: (traceId: string) => void }) {
  const [range, setRange] = useState<(typeof RANGES)[number]>("24h");
  const [data, setData] = useState<OverviewData | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setData(await api.overview(range));
      setError(null);
    } catch (e) {
      setError(e as Error);
    } finally {
      setLoading(false);
    }
  }, [range]);

  useEffect(() => {
    void load();
  }, [load]);

  const successRate = data ? pct(data.success, data.requests) : "—";

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs muted">
          Traffic, latency and failures derived from stored request traces. No provider prompt or
          completion content is stored or displayed.
        </p>
        <div
          role="group"
          aria-label="Time range"
          className="flex gap-1 rounded-full border border-[color:var(--surface-border)] p-0.5"
        >
          {RANGES.map((r) => (
            <button
              key={r}
              onClick={() => setRange(r)}
              aria-pressed={range === r}
              className={cx(
                "rounded-full px-3 py-1 text-xs transition-colors duration-[var(--dur-fast)]",
                range === r
                  ? "bg-[color:var(--signal-dim)] text-[color:var(--signal)]"
                  : "text-[color:var(--text-muted)] hover:text-[color:var(--text-body)]",
              )}
            >
              {r}
            </button>
          ))}
        </div>
      </div>

      <ErrorNote error={error} onRetry={() => void load()} />
      {loading && !data && <Loading label="Reading traffic" />}

      {data && (
        <>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <Metric
              label="Requests"
              value={grouped(data.requests)}
              hint={`${grouped(data.success)} ok · ${grouped(data.errors)} failed`}
            />
            <Metric
              label="Success rate"
              value={successRate}
              tone={data.requests === 0 ? "idle" : data.errors === 0 ? "ok" : "bad"}
              hint={`${grouped(data.errors)} failed`}
            />
            <Metric label="p95 latency" value={ms(data.p95_latency_ms)} hint="end-to-end, all attempts" />
            <Metric
              label="Fallback events"
              value={grouped(data.fallback_events)}
              tone={data.fallback_events > 0 ? "bad" : "idle"}
              hint="requests that moved past the primary"
            />
          </div>

          <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
            <Panel
              className="xl:col-span-2"
              title="Traffic"
              subtitle={`${data.range} · successful versus failed per bucket`}
            >
              <TrafficChart points={data.series} />
            </Panel>

            <Panel title="Fleet" subtitle="Configured capacity">
              <dl className="flex flex-col gap-3">
                <Stat label="Published routes" value={grouped(data.published_routes)} />
                <Stat label="Active keys" value={grouped(data.active_keys)} />
                <Stat label="Providers" value={grouped(data.providers.length)} />
                <Stat label="Tokens in / out" value={`${compact(data.tokens_in)} / ${compact(data.tokens_out)}`} />
              </dl>
            </Panel>
          </div>

          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            <Panel title="Provider health" subtitle="Last bounded connection check">
              {data.providers.length === 0 ? (
                <EmptyState
                  title="No providers configured"
                  body="Add an upstream on the Providers page to start routing traffic."
                />
              ) : (
                <Table head={["Provider", "Host", "Models", "State", "Checked"]}>
                  {data.providers.map((p) => (
                    <Row key={p.id}>
                      <Cell>{p.name}</Cell>
                      <Cell mono>{p.base_url}</Cell>
                      <Cell mono>{p.models.length}</Cell>
                      <Cell>
                        <Badge tone={tone(p.last_test_state)}>{p.last_test_state}</Badge>
                      </Cell>
                      <Cell className="muted">{relative(p.last_test_at)}</Cell>
                    </Row>
                  ))}
                </Table>
              )}
            </Panel>

            <Panel title="Recent failures" subtitle="Newest five non-successful requests">
              {data.recent_failures.length === 0 ? (
                <EmptyState
                  title="No failures recorded"
                  body="Every request in this window completed successfully. Nothing is hidden here — this list is populated from the same rows the Requests page reads."
                />
              ) : (
                <Table head={["Trace", "Alias", "Status", "Code", "Latency", "When"]}>
                  {data.recent_failures.map((f) => (
                    <Row key={f.trace_id} onClick={() => onOpenTrace(f.trace_id)}>
                      <Cell mono>{f.trace_id}</Cell>
                      <Cell mono>{f.alias}</Cell>
                      <Cell>
                        <Badge tone={tone(f.status)}>{f.status}</Badge>
                      </Cell>
                      <Cell mono>{f.error_code || "—"}</Cell>
                      <Cell mono>{ms(f.duration_ms)}</Cell>
                      <Cell className="muted">{relative(f.created_at)}</Cell>
                    </Row>
                  ))}
                </Table>
              )}
            </Panel>
          </div>
        </>
      )}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-4 border-b border-[color:var(--surface-border)]/60 pb-2 last:border-0 last:pb-0">
      <dt className="text-xs muted">{label}</dt>
      <dd className="text-sm font-semibold tabular-nums text-[color:var(--text-strong)]">{value}</dd>
    </div>
  );
}

/** A stacked bar chart. Deliberately plain SVG: no charting dependency, and the
 *  numbers are readable as text for anyone who cannot see the bars. */
function TrafficChart({ points }: { points: Array<{ t: string; ok: number; failed: number }> }) {
  if (points.length === 0) {
    return (
      <EmptyState
        title="No traffic in this window"
        body="The chart draws from recorded requests. Send one to see it fill in."
      />
    );
  }
  const peak = Math.max(1, ...points.map((p) => p.ok + p.failed));

  return (
    <figure className="flex flex-col gap-3">
      <div className="flex h-40 items-end gap-1" role="img" aria-label="Traffic by bucket">
        {points.map((p, i) => {
          const total = p.ok + p.failed;
          const heightPct = (total / peak) * 100;
          const failPct = total === 0 ? 0 : (p.failed / total) * 100;
          return (
            <div
              key={`${p.t}-${i}`}
              className="group relative flex-1 rounded-t-sm bg-[color:var(--signal-strong)]/70 transition-opacity hover:opacity-100"
              style={{ height: `${Math.max(heightPct, 2)}%` }}
              title={`${total} requests · ${p.failed} failed · ${new Date(p.t).toISOString()}`}
            >
              {/* The failure slice sits on top, so a bad bucket reads at a glance. */}
              <div
                className="absolute inset-x-0 top-0 rounded-t-sm bg-[color:var(--bad)]"
                style={{ height: `${failPct}%` }}
              />
            </div>
          );
        })}
      </div>
      <figcaption className="flex flex-wrap items-center justify-between gap-2 text-[11px] faint">
        <span>{new Date(points[0]!.t).toISOString().slice(0, 16).replace("T", " ")}Z</span>
        <span className="flex items-center gap-3">
          <span className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-sm bg-[color:var(--signal-strong)]/70" /> ok
          </span>
          <span className="flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-sm bg-[color:var(--bad)]" /> failed
          </span>
          <span>peak {peak}/bucket</span>
        </span>
        <span>{new Date(points[points.length - 1]!.t).toISOString().slice(0, 16).replace("T", " ")}Z</span>
      </figcaption>
    </figure>
  );
}
