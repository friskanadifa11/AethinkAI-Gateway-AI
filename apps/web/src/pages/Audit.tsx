// Audit log: what operators changed and when. Read-only by design — there is no
// endpoint that edits or deletes an audit entry.

import { useCallback, useEffect, useState } from "react";
import { api } from "../lib/api";
import { relative, stamp } from "../lib/format";
import { Badge, Button, Cell, EmptyState, ErrorNote, Loading, Panel, Row, Select, Table } from "../components/ui";

export function AuditPage() {
  const [events, setEvents] = useState<Array<{ actor: string; action: string; target: string; created_at: string }> | null>(null);
  const [limit, setLimit] = useState("50");
  const [error, setError] = useState<Error | null>(null);

  const load = useCallback(async () => {
    try {
      setEvents(await api.audit(Number(limit)));
      setError(null);
    } catch (e) {
      setError(e as Error);
    }
  }, [limit]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <div className="flex flex-col gap-4">
      <Panel
        title="Operator activity"
        subtitle="Append-only record of configuration changes and sign-ins"
        actions={
          <div className="flex items-center gap-2">
            <Select value={limit} onChange={(e) => setLimit(e.target.value)} className="w-28">
              <option value="25">25 rows</option>
              <option value="50">50 rows</option>
              <option value="100">100 rows</option>
              <option value="200">200 rows</option>
            </Select>
            <Button size="sm" onClick={() => void load()}>
              Refresh
            </Button>
          </div>
        }
      >
        <ErrorNote error={error} onRetry={() => void load()} />
        {events === null && !error ? (
          <Loading label="Loading audit trail" />
        ) : events && events.length === 0 ? (
          <EmptyState
            title="Nothing recorded yet"
            body="Signing in, creating a route, rotating a key — each of those writes a row here."
          />
        ) : (
          <Table head={["When", "Actor", "Action", "Target"]}>
            {events?.map((e, i) => (
              <Row key={`${e.created_at}-${i}`}>
                <Cell className="muted" title={stamp(e.created_at)}>
                  {relative(e.created_at)}
                </Cell>
                <Cell>{e.actor || <span className="faint">system</span>}</Cell>
                <Cell>
                  <Badge tone={e.action.includes("deleted") || e.action.includes("revoked") ? "warn" : "idle"}>
                    {e.action}
                  </Badge>
                </Cell>
                <Cell mono>{e.target || "—"}</Cell>
              </Row>
            ))}
          </Table>
        )}
      </Panel>
    </div>
  );
}
