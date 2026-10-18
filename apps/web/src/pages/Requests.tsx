// Requests: the operator's forensic view. One row per accepted request; opening
// a row shows every upstream attempt that request made, in order, so a fallback
// is visible rather than inferred.

import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type RequestLog, type TraceDetail } from "../lib/api";
import { grouped, ms, relative, stamp, tone } from "../lib/format";
import {
  Badge,
  Button,
  Cell,
  EmptyState,
  ErrorNote,
  Field,
  Input,
  Loading,
  Modal,
  Panel,
  Row,
  Select,
  Table,
  cx,
  useToast,
} from "../components/ui";

const LATENCY_TONE = (ms_: number): "ok" | "warn" | "bad" =>
  ms_ < 1500 ? "ok" : ms_ < 5000 ? "warn" : "bad";

export function RequestsPage({
  focusTrace,
  onFocusHandled,
}: {
  focusTrace?: string;
  onFocusHandled?: () => void;
}) {
  const toast = useToast();
  const [rows, setRows] = useState<RequestLog[] | null>(null);
  const [total, setTotal] = useState(0);
  const [aliases, setAliases] = useState<string[]>([]);
  const [error, setError] = useState<Error | null>(null);
  const [detail, setDetail] = useState<TraceDetail | null>(null);

  const [since, setSince] = useState("24h");
  const [status, setStatus] = useState("");
  const [alias, setAlias] = useState("");
  const [search, setSearch] = useState("");
  const [offset, setOffset] = useState(0);
  const limit = 50;

  const load = useCallback(async () => {
    try {
      const result = await api.listRequests({ since, status, alias, q: search, limit, offset });
      setRows(result.requests);
      setTotal(result.total);
      setAliases(result.aliases);
      setError(null);
    } catch (e) {
      setError(e as Error);
    }
  }, [since, status, alias, search, offset]);

  useEffect(() => {
    void load();
  }, [load]);

  const openTrace = useCallback(
    async (traceId: string) => {
      try {
        setDetail(await api.traceDetail(traceId));
      } catch (e) {
        toast.push("bad", e instanceof ApiError ? e.message : "That trace could not be read.");
      }
    },
    [toast],
  );

  // Deep link support: the Overview page can request a specific trace.
  useEffect(() => {
    if (focusTrace) {
      void openTrace(focusTrace);
      onFocusHandled?.();
    }
  }, [focusTrace, openTrace, onFocusHandled]);

  const pages = Math.max(1, Math.ceil(total / limit));
  const currentPage = Math.floor(offset / limit) + 1;

  return (
    <div className="flex flex-col gap-4">
      <Panel
        title="Filters"
        subtitle="From stored metadata only — no prompt or completion content is kept"
      >
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Field label="Window">
            <Select
              value={since}
              onChange={(e) => {
                setOffset(0);
                setSince(e.target.value);
              }}
            >
              <option value="30m">Last 30 minutes</option>
              <option value="6h">Last 6 hours</option>
              <option value="24h">Last 24 hours</option>
              <option value="7d">Last 7 days</option>
              <option value="30d">Last 30 days</option>
            </Select>
          </Field>
          <Field label="Status">
            <Select
              value={status}
              onChange={(e) => {
                setOffset(0);
                setStatus(e.target.value);
              }}
            >
              <option value="">Any</option>
              <option value="ok">Succeeded</option>
              <option value="error">Failed</option>
              <option value="rejected">Rejected before dispatch</option>
            </Select>
          </Field>
          <Field label="Alias">
            <Select
              value={alias}
              onChange={(e) => {
                setOffset(0);
                setAlias(e.target.value);
              }}
            >
              <option value="">Any</option>
              {aliases.map((a) => (
                <option key={a} value={a}>
                  {a}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Trace or key label" hint="Substring match.">
            <Input
              value={search}
              onChange={(e) => {
                setOffset(0);
                setSearch(e.target.value);
              }}
              placeholder="8f3a…"
            />
          </Field>
        </div>
      </Panel>

      <ErrorNote error={error} onRetry={() => void load()} />

      <Panel
        title="Request log"
        subtitle={`${grouped(total)} matching request${total === 1 ? "" : "s"}`}
        actions={
          <div className="flex items-center gap-2">
            <Button size="sm" onClick={() => setOffset(Math.max(0, offset - limit))} disabled={offset === 0}>
              Newer
            </Button>
            <span className="text-[11px] faint">
              page {currentPage} / {pages}
            </span>
            <Button
              size="sm"
              onClick={() => setOffset(offset + limit)}
              disabled={offset + limit >= total}
            >
              Older
            </Button>
          </div>
        }
      >
        {rows === null && !error ? (
          <Loading label="Loading requests" />
        ) : rows && rows.length === 0 ? (
          <EmptyState
            title="Nothing in this window"
            body="Widen the time range, or send a request to the public API and it will appear here."
          />
        ) : (
          <Table head={["Trace", "When", "Alias", "Key", "Provider", "Status", "Latency", "TTFT", "Tokens", "Fallbacks"]}>
            {rows?.map((r) => (
              <Row key={r.trace_id} onClick={() => void openTrace(r.trace_id)}>
                <Cell mono>{r.trace_id}</Cell>
                <Cell className="muted" title={stamp(r.created_at)}>
                  {relative(r.created_at)}
                </Cell>
                <Cell mono>{r.alias}</Cell>
                <Cell className="muted">{r.key_label || "—"}</Cell>
                <Cell>
                  {r.provider_name ? (
                    <span>
                      {r.provider_name}
                      <span className="ident ml-1.5 faint">{r.upstream_model}</span>
                    </span>
                  ) : (
                    "—"
                  )}
                </Cell>
                <Cell>
                  <span className="flex items-center gap-2">
                    <Badge tone={tone(r.status)}>{r.status}</Badge>
                    {r.error_code && <span className="ident faint">{r.error_code}</span>}
                  </span>
                </Cell>
                <Cell mono>
                  <span className={cx(
                    LATENCY_TONE(r.duration_ms) === "ok" && "text-[color:var(--ok)]",
                    LATENCY_TONE(r.duration_ms) === "warn" && "text-[color:var(--warn)]",
                    LATENCY_TONE(r.duration_ms) === "bad" && "text-[color:var(--bad)]",
                  )}>
                    {ms(r.duration_ms)}
                  </span>
                </Cell>
                <Cell mono>{r.streamed ? ms(r.ttft_ms) : "—"}</Cell>
                <Cell mono>
                  {grouped(r.input_tokens)}
                  <span className="faint"> / {grouped(r.output_tokens)}</span>
                </Cell>
                <Cell>
                  {r.fallback_count > 0 ? <Badge tone="warn">{r.fallback_count}</Badge> : <span className="faint">0</span>}
                </Cell>
              </Row>
            ))}
          </Table>
        )}
      </Panel>

      <Modal
        open={detail !== null}
        title={detail ? `Trace ${detail.trace_id}` : ""}
        onClose={() => setDetail(null)}
        footer={<Button onClick={() => setDetail(null)}>Close</Button>}
      >
        {detail && (
          <div className="flex flex-col gap-4">
            <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3">
              <Detail label="Alias" value={detail.alias} mono />
              <Detail label="Status" value={`${detail.status} (${detail.http_status})`} />
              <Detail label="When" value={stamp(detail.created_at)} />
              <Detail label="Key" value={detail.key_label || "—"} />
              <Detail label="Provider" value={detail.provider_name || "—"} />
              <Detail label="Model" value={detail.upstream_model || "—"} mono />
              <Detail label="Duration" value={ms(detail.duration_ms)} />
              <Detail label="TTFT" value={detail.streamed ? ms(detail.ttft_ms) : "not streamed"} />
              <Detail label="Tokens" value={`${grouped(detail.input_tokens)} in / ${grouped(detail.output_tokens)} out`} />
            </dl>

            <div>
              <p className="mb-2 text-xs font-medium text-[color:var(--text-muted)]">
                Attempts, in the order the router tried them
              </p>
              {detail.attempts.length === 0 ? (
                <p className="text-xs muted">
                  This request was refused before any provider was contacted.
                </p>
              ) : (
                <ol className="flex flex-col gap-2">
                  {detail.attempts.map((a) => (
                    <li
                      key={a.attempt_no}
                      className={cx(
                        "panel-raised flex flex-wrap items-center justify-between gap-3 px-3 py-2.5",
                        a.status === "ok" && "signal-bar pl-4",
                      )}
                    >
                      <span className="flex items-center gap-3">
                        <span className="text-[11px] font-semibold faint">#{a.attempt_no}</span>
                        <span className="text-sm text-[color:var(--text-strong)]">{a.provider_name}</span>
                        <span className="ident">{a.upstream_model}</span>
                      </span>
                      <span className="flex items-center gap-3">
                        <Badge tone={tone(a.status)}>
                          {a.status === "ok" ? "answered" : a.error_code || "failed"}
                        </Badge>
                        <span className="ident">{a.http_status || "—"}</span>
                        <span className="text-[11px] muted">{ms(a.duration_ms)}</span>
                      </span>
                    </li>
                  ))}
                </ol>
              )}
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}

function Detail({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <dt className="text-[10px] font-medium uppercase tracking-wider faint">{label}</dt>
      <dd className={cx("mt-0.5 text-xs text-[color:var(--text-body)]", mono && "ident")}>{value}</dd>
    </div>
  );
}
