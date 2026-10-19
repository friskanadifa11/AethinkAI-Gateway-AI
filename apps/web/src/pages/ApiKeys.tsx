// API keys: what callers authenticate with.
//
// The plaintext key exists in exactly one place for exactly one moment — the
// response to the request that created it. There is no "reveal" action, and
// that absence is the security property, not an oversight.
//
// The alias list is a picker, not a text box. The allowlist fails closed at
// request time, so a mistyped alias used to produce a key that could reach
// nothing and say nothing about it. The server rejects unknown aliases too, but
// there is no reason to make an operator type what the console already knows.

import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type ClientKey, type KeyUsage, type RequestLog } from "../lib/api";
import { grouped, keyPrefix, relative } from "../lib/format";
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
  OneTimeSecret,
  Panel,
  Row,
  Select,
  Table,
  useToast,
} from "../components/ui";

type Draft = {
  label: string;
  owner_label: string;
  rpm: string;
  max_concurrency: string;
  token_quota: string;
  expires_in_days: string;
  allowed_aliases: string[];
  enabled: boolean;
};

const EMPTY: Draft = {
  label: "",
  owner_label: "",
  rpm: "60",
  max_concurrency: "4",
  token_quota: "0",
  expires_in_days: "0",
  allowed_aliases: [],
  enabled: true,
};

const RANGES = [
  { value: "1h", label: "Last hour" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
];

function isActive(k: ClientKey): boolean {
  return k.enabled && !k.revoked_at && (!k.expires_at || new Date(k.expires_at) > new Date());
}

function stateOf(k: ClientKey): string {
  if (!k.enabled || k.revoked_at) return "revoked";
  if (k.expires_at && new Date(k.expires_at) <= new Date()) return "expired";
  return "active";
}

// AliasPicker renders the published catalogue as checkboxes. An empty selection
// is a real choice - it means every alias, current and future - so it is stated
// in words rather than left as an empty list the operator has to interpret.
function AliasPicker({
  published,
  selected,
  onToggle,
}: {
  published: string[];
  selected: string[];
  onToggle: (alias: string) => void;
}) {
  if (published.length === 0) {
    return (
      <p className="text-[11px] faint">
        No published routes yet. Publish one on the Routing page before limiting a key.
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-2">
      <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
        {published.map((alias) => (
          <label key={alias} className="flex items-center gap-2 text-xs muted">
            <input
              type="checkbox"
              checked={selected.includes(alias)}
              onChange={() => onToggle(alias)}
              className="h-3.5 w-3.5 accent-[color:var(--signal-strong)]"
            />
            <span className="truncate font-mono" title={alias}>
              {alias}
            </span>
          </label>
        ))}
      </div>
      <p className="text-[11px] faint">
        {selected.length === 0
          ? "Nothing ticked: this key may call every published alias."
          : `${selected.length} alias${selected.length === 1 ? "" : "es"} ticked; every other alias is refused.`}
      </p>
    </div>
  );
}

export function ApiKeysPage() {
  const toast = useToast();
  const [keys, setKeys] = useState<ClientKey[] | null>(null);
  const [published, setPublished] = useState<string[]>([]);
  const [error, setError] = useState<Error | null>(null);

  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<ClientKey | null>(null);
  const [draft, setDraft] = useState<Draft>(EMPTY);
  const [saving, setSaving] = useState(false);
  const [minted, setMinted] = useState<string | null>(null);

  const [rotating, setRotating] = useState<ClientKey | null>(null);
  const [graceHours, setGraceHours] = useState("24");
  const [revoking, setRevoking] = useState<ClientKey | null>(null);

  const [detail, setDetail] = useState<ClientKey | null>(null);
  const [usage, setUsage] = useState<KeyUsage | null>(null);
  const [recent, setRecent] = useState<RequestLog[]>([]);
  const [detailRange, setDetailRange] = useState("24h");
  const [detailBusy, setDetailBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const [list, routes] = await Promise.all([api.listKeys(), api.listRoutes()]);
      setKeys(list);
      setPublished(routes.published.map((r) => r.alias));
      setError(null);
    } catch (e) {
      setError(e as Error);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const openCreate = () => {
    setDraft({ ...EMPTY, allowed_aliases: [] });
    setEditing(null);
    setCreating(true);
  };

  const openEdit = (k: ClientKey) => {
    setDraft({
      label: k.label,
      owner_label: k.owner_label,
      rpm: String(k.rpm),
      max_concurrency: String(k.max_concurrency),
      token_quota: String(k.token_quota),
      // Empty means "keep the current expiry": a plain 0 would silently make
      // an expiring key permanent.
      expires_in_days: "",
      allowed_aliases: [...k.allowed_aliases],
      enabled: k.enabled && !k.revoked_at,
    });
    setCreating(false);
    setEditing(k);
  };

  const toggleAlias = (alias: string) => {
    setDraft((d) => ({
      ...d,
      allowed_aliases: d.allowed_aliases.includes(alias)
        ? d.allowed_aliases.filter((a) => a !== alias)
        : [...d.allowed_aliases, alias],
    }));
  };

  const create = async () => {
    setSaving(true);
    try {
      const result = await api.createKey({
        label: draft.label.trim(),
        owner_label: draft.owner_label.trim(),
        rpm: Number(draft.rpm) || 0,
        max_concurrency: Number(draft.max_concurrency) || 0,
        token_quota: Number(draft.token_quota) || 0,
        expires_in_days: Number(draft.expires_in_days) || 0,
        allowed_aliases: draft.allowed_aliases,
      });
      setCreating(false);
      setMinted(result.token);
      await load();
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "The key could not be created.");
    } finally {
      setSaving(false);
    }
  };

  const save = async () => {
    if (!editing) return;
    setSaving(true);
    try {
      const patch: Record<string, unknown> = {
        label: draft.label.trim(),
        owner_label: draft.owner_label.trim(),
        rpm: Number(draft.rpm) || 0,
        max_concurrency: Number(draft.max_concurrency) || 0,
        token_quota: Number(draft.token_quota) || 0,
        allowed_aliases: draft.allowed_aliases,
        enabled: draft.enabled,
      };
      if (draft.expires_in_days.trim() !== "") {
        patch.expires_in_days = Number(draft.expires_in_days) || 0;
      }
      await api.updateKey(editing.id, patch);
      toast.push("ok", `${draft.label.trim()} updated.`);
      setEditing(null);
      await load();
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "The key could not be updated.");
    } finally {
      setSaving(false);
    }
  };

  const openDetail = async (k: ClientKey, range = detailRange) => {
    setDetail(k);
    setDetailBusy(true);
    try {
      const [data, log] = await Promise.all([
        api.getKey(k.id, range),
        api.listRequests({ key_id: k.id, limit: 8, since: "30d" }),
      ]);
      setUsage(data.usage);
      setRecent(log.requests);
      setDetailRange(range);
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "Usage could not be loaded.");
      setUsage(null);
      setRecent([]);
    } finally {
      setDetailBusy(false);
    }
  };

  const rotate = async () => {
    if (!rotating) return;
    try {
      const result = await api.rotateKey(rotating.id, Number(graceHours) || 24);
      setRotating(null);
      setMinted(result.token);
      toast.push("ok", `Old key ${result.replaced} expires ${relative(result.grace_until)}-ish.`);
      await load();
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "The key could not be rotated.");
    }
  };

  const revoke = async () => {
    if (!revoking) return;
    try {
      await api.revokeKey(revoking.id);
      toast.push("ok", `${revoking.label} revoked.`);
      setRevoking(null);
      await load();
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "The key could not be revoked.");
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-prose text-xs muted">
          Keys are stored as an HMAC-SHA256 digest plus a short lookup prefix. A key that is lost
          cannot be recovered — rotate it and deploy the replacement instead.
        </p>
        <Button variant="primary" onClick={openCreate}>
          Create key
        </Button>
      </div>

      <ErrorNote error={error} onRetry={() => void load()} />

      <Panel title="Client keys" subtitle="Every key ever issued, including revoked ones">
        {keys === null && !error ? (
          <Loading label="Loading keys" />
        ) : keys && keys.length === 0 ? (
          <EmptyState
            title="No keys issued"
            body="Without a key nobody can call the public API. Create one to hand out access."
          />
        ) : (
          <Table
            head={["Label", "Prefix", "State", "RPM", "Concurrency", "Tokens used", "Aliases", "Last used", "Actions"]}
          >
            {keys?.map((k) => {
              const active = isActive(k);
              return (
                <Row key={k.id}>
                  <Cell>
                    <span className="font-medium text-[color:var(--text-strong)]">{k.label}</span>
                    {k.owner_label && <span className="ml-2 text-[11px] faint">{k.owner_label}</span>}
                  </Cell>
                  <Cell mono>{keyPrefix(k.prefix)}</Cell>
                  <Cell>
                    <Badge tone={active ? "ok" : "idle"}>{stateOf(k)}</Badge>
                  </Cell>
                  <Cell mono>{k.rpm === 0 ? "∞" : grouped(k.rpm)}</Cell>
                  <Cell mono>{k.max_concurrency === 0 ? "∞" : grouped(k.max_concurrency)}</Cell>
                  <Cell mono>
                    {grouped(k.tokens_used)}
                    {k.token_quota > 0 && <span className="faint"> / {grouped(k.token_quota)}</span>}
                  </Cell>
                  <Cell mono title={k.allowed_aliases.join(", ")}>
                    {k.allowed_aliases.length === 0 ? "all" : k.allowed_aliases.join(", ")}
                  </Cell>
                  <Cell className="muted">{relative(k.last_used_at)}</Cell>
                  <Cell>
                    <div className="flex flex-wrap gap-1.5">
                      <Button size="sm" onClick={() => void openDetail(k)}>
                        Usage
                      </Button>
                      <Button size="sm" onClick={() => openEdit(k)}>
                        Edit
                      </Button>
                      <Button
                        size="sm"
                        onClick={() => {
                          setRotating(k);
                          setGraceHours("24");
                        }}
                        disabled={!active}
                      >
                        Rotate
                      </Button>
                      <Button size="sm" variant="danger" onClick={() => setRevoking(k)} disabled={!active}>
                        Revoke
                      </Button>
                    </div>
                  </Cell>
                </Row>
              );
            })}
          </Table>
        )}
      </Panel>

      <Modal
        open={creating}
        title="Create API key"
        onClose={() => setCreating(false)}
        footer={
          <>
            <Button onClick={() => setCreating(false)}>Cancel</Button>
            <Button variant="primary" busy={saving} onClick={() => void create()} disabled={draft.label.trim().length < 2}>
              Create key
            </Button>
          </>
        }
      >
        <KeyForm draft={draft} setDraft={setDraft} published={published} onToggleAlias={toggleAlias} />
      </Modal>

      <Modal
        open={editing !== null}
        title={`Edit ${editing?.label ?? ""}`}
        onClose={() => setEditing(null)}
        footer={
          <>
            <Button onClick={() => setEditing(null)}>Cancel</Button>
            <Button variant="primary" busy={saving} onClick={() => void save()} disabled={draft.label.trim().length < 2}>
              Save changes
            </Button>
          </>
        }
      >
        <KeyForm
          draft={draft}
          setDraft={setDraft}
          published={published}
          onToggleAlias={toggleAlias}
          editing
        />
      </Modal>

      <Modal
        open={detail !== null}
        title={`Usage — ${detail?.label ?? ""}`}
        onClose={() => {
          setDetail(null);
          setUsage(null);
          setRecent([]);
        }}
        footer={
          <>
            <Select value={detailRange} onChange={(e) => void openDetail(detail as ClientKey, e.target.value)}>
              {RANGES.map((r) => (
                <option key={r.value} value={r.value}>
                  {r.label}
                </option>
              ))}
            </Select>
            <Button
              onClick={() => {
                setDetail(null);
                setUsage(null);
                setRecent([]);
              }}
            >
              Close
            </Button>
          </>
        }
      >
        {detailBusy && !usage ? (
          <Loading label="Loading usage" />
        ) : usage ? (
          <div className="flex flex-col gap-4">
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <Stat label="Requests" value={grouped(usage.requests)} />
              <Stat label="Errors" value={grouped(usage.errors)} tone={usage.errors > 0 ? "warn" : undefined} />
              <Stat label="Tokens in" value={grouped(usage.tokens_in)} />
              <Stat label="Tokens out" value={grouped(usage.tokens_out)} />
            </div>

            {usage.aliases.length === 0 ? (
              <p className="text-xs muted">No traffic from this key in this window.</p>
            ) : (
              <Table head={["Alias", "Requests", "Tokens"]}>
                {usage.aliases.map((a) => (
                  <Row key={a.alias}>
                    <Cell mono>{a.alias}</Cell>
                    <Cell mono>{grouped(a.requests)}</Cell>
                    <Cell mono>{grouped(a.tokens)}</Cell>
                  </Row>
                ))}
              </Table>
            )}

            <div>
              <p className="mb-2 text-[11px] font-semibold uppercase tracking-widest faint">Recent requests</p>
              {recent.length === 0 ? (
                <p className="text-xs muted">Nothing recorded for this key.</p>
              ) : (
                <div className="flex flex-col gap-1">
                  {recent.map((r) => (
                    <div key={r.trace_id} className="flex items-center justify-between gap-3 text-[11px]">
                      <span className="truncate font-mono faint" title={r.trace_id}>
                        {r.trace_id.slice(0, 18)}
                      </span>
                      <span className="truncate font-mono">{r.alias}</span>
                      <Badge tone={r.status === "ok" ? "ok" : r.status === "rejected" ? "idle" : "bad"}>
                        {r.status}
                      </Badge>
                      <span className="w-20 shrink-0 text-right font-mono muted">
                        {grouped(r.input_tokens + r.output_tokens)}
                      </span>
                      <span className="w-20 shrink-0 text-right muted">{relative(r.created_at)}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        ) : (
          <p className="text-xs muted">Usage unavailable.</p>
        )}
      </Modal>

      <Modal open={minted !== null} title="Your new API key" onClose={() => setMinted(null)} footer={null}>
        {minted && <OneTimeSecret token={minted} onDone={() => setMinted(null)} />}
      </Modal>

      <Modal
        open={rotating !== null}
        title={`Rotate ${rotating?.label ?? ""}`}
        onClose={() => setRotating(null)}
        footer={
          <>
            <Button onClick={() => setRotating(null)}>Cancel</Button>
            <Button variant="primary" onClick={() => void rotate()}>
              Create replacement
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-3">
          <p className="text-xs muted">
            The replacement inherits this key&apos;s limits and aliases. The current key keeps working
            during the grace window, then stops. The old secret is never shown again.
          </p>
          <Field label="Grace window (hours)" hint="0 cuts the old key over immediately. Maximum 720.">
            <Input value={graceHours} inputMode="numeric" onChange={(e) => setGraceHours(e.target.value)} />
          </Field>
        </div>
      </Modal>

      <Modal
        open={revoking !== null}
        title="Revoke key"
        onClose={() => setRevoking(null)}
        footer={
          <>
            <Button onClick={() => setRevoking(null)}>Keep it</Button>
            <Button variant="danger" onClick={() => void revoke()}>
              Revoke now
            </Button>
          </>
        }
      >
        <p className="text-sm muted">
          {revoking?.label} stops working immediately. Historical traces that used it are kept for
          attribution.
        </p>
      </Modal>
    </div>
  );
}

function Stat({ label, value, tone }: { label: string; value: string; tone?: "warn" }) {
  return (
    <div className="rounded-[var(--radius-sm)] border border-[color:var(--surface-border)] px-3 py-2">
      <p className="text-[10px] uppercase tracking-widest faint">{label}</p>
      <p
        className={
          tone === "warn"
            ? "font-mono text-sm text-[color:var(--warn)]"
            : "font-mono text-sm text-[color:var(--text-strong)]"
        }
      >
        {value}
      </p>
    </div>
  );
}

function KeyForm({
  draft,
  setDraft,
  published,
  onToggleAlias,
  editing,
}: {
  draft: Draft;
  setDraft: (d: Draft) => void;
  published: string[];
  onToggleAlias: (alias: string) => void;
  editing?: boolean;
}) {
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <Field label="Label" hint="What this key is for. Shown in traces.">
        <Input
          value={draft.label}
          onChange={(e) => setDraft({ ...draft, label: e.target.value })}
          placeholder="Production app"
          autoComplete="off"
        />
      </Field>
      <Field label="Owner" hint="Optional team or customer name.">
        <Input
          value={draft.owner_label}
          onChange={(e) => setDraft({ ...draft, owner_label: e.target.value })}
          placeholder="acme"
          autoComplete="off"
        />
      </Field>
      <Field label="Requests per minute" hint="0 means unlimited.">
        <Input value={draft.rpm} inputMode="numeric" onChange={(e) => setDraft({ ...draft, rpm: e.target.value })} />
      </Field>
      <Field label="Max concurrency" hint="0 means unlimited.">
        <Input
          value={draft.max_concurrency}
          inputMode="numeric"
          onChange={(e) => setDraft({ ...draft, max_concurrency: e.target.value })}
        />
      </Field>
      <Field label="Token quota" hint="0 means unmetered. Counts input plus output tokens.">
        <Input
          value={draft.token_quota}
          inputMode="numeric"
          onChange={(e) => setDraft({ ...draft, token_quota: e.target.value })}
        />
      </Field>
      <Field
        label="Expires in days"
        hint={editing ? "Leave empty to keep the current expiry. 0 means never." : "0 means the key never expires."}
      >
        <Input
          value={draft.expires_in_days}
          inputMode="numeric"
          onChange={(e) => setDraft({ ...draft, expires_in_days: e.target.value })}
        />
      </Field>

      <div className="sm:col-span-2">
        <Field label="Allowed aliases" hint="Tick the models this customer may call.">
          <AliasPicker published={published} selected={draft.allowed_aliases} onToggle={onToggleAlias} />
        </Field>
      </div>

      {editing && (
        <label className="flex items-center gap-2 text-xs muted sm:col-span-2">
          <input
            type="checkbox"
            checked={draft.enabled}
            onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })}
            className="h-4 w-4 accent-[color:var(--signal-strong)]"
          />
          Enabled — unticking stops the key without revoking it.
        </label>
      )}
    </div>
  );
}
