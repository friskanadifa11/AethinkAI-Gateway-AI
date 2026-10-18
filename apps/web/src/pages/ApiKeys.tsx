// API keys: what callers authenticate with.
//
// The plaintext key exists in exactly one place for exactly one moment — the
// response to the request that created it. There is no "reveal" action, and
// that absence is the security property, not an oversight.

import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type ClientKey } from "../lib/api";
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
  allowed_aliases: string;
};

const EMPTY: Draft = {
  label: "",
  owner_label: "",
  rpm: "60",
  max_concurrency: "4",
  token_quota: "0",
  expires_in_days: "0",
  allowed_aliases: "",
};

export function ApiKeysPage() {
  const toast = useToast();
  const [keys, setKeys] = useState<ClientKey[] | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [creating, setCreating] = useState(false);
  const [draft, setDraft] = useState<Draft>(EMPTY);
  const [saving, setSaving] = useState(false);
  const [minted, setMinted] = useState<string | null>(null);
  const [rotating, setRotating] = useState<ClientKey | null>(null);
  const [graceHours, setGraceHours] = useState("24");
  const [revoking, setRevoking] = useState<ClientKey | null>(null);

  const load = useCallback(async () => {
    try {
      setKeys(await api.listKeys());
      setError(null);
    } catch (e) {
      setError(e as Error);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

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
        allowed_aliases: draft.allowed_aliases
          .split(",")
          .map((a) => a.trim())
          .filter(Boolean),
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

  const isActive = (k: ClientKey) =>
    k.enabled && !k.revoked_at && (!k.expires_at || new Date(k.expires_at) > new Date());

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-prose text-xs muted">
          Keys are stored as an HMAC-SHA256 digest plus a short lookup prefix. A key that is lost
          cannot be recovered — rotate it and deploy the replacement instead.
        </p>
        <Button variant="primary" onClick={() => { setDraft(EMPTY); setCreating(true); }}>
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
                    <Badge tone={active ? "ok" : "idle"}>
                      {!k.enabled || k.revoked_at
                        ? "revoked"
                        : k.expires_at && new Date(k.expires_at) <= new Date()
                          ? "expired"
                          : "active"}
                    </Badge>
                  </Cell>
                  <Cell mono>{k.rpm === 0 ? "∞" : grouped(k.rpm)}</Cell>
                  <Cell mono>{k.max_concurrency === 0 ? "∞" : grouped(k.max_concurrency)}</Cell>
                  <Cell mono>
                    {grouped(k.tokens_used)}
                    {k.token_quota > 0 && <span className="faint"> / {grouped(k.token_quota)}</span>}
                  </Cell>
                  <Cell mono>{k.allowed_aliases.length === 0 ? "all" : k.allowed_aliases.length}</Cell>
                  <Cell className="muted">{relative(k.last_used_at)}</Cell>
                  <Cell>
                    <div className="flex gap-1.5">
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
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label="Label" hint="What this key is for. Shown in traces.">
            <Input value={draft.label} onChange={(e) => setDraft({ ...draft, label: e.target.value })} placeholder="Production app" autoComplete="off" />
          </Field>
          <Field label="Owner" hint="Optional team or customer name.">
            <Input value={draft.owner_label} onChange={(e) => setDraft({ ...draft, owner_label: e.target.value })} placeholder="acme" autoComplete="off" />
          </Field>
          <Field label="Requests per minute" hint="0 means unlimited.">
            <Input value={draft.rpm} inputMode="numeric" onChange={(e) => setDraft({ ...draft, rpm: e.target.value })} />
          </Field>
          <Field label="Max concurrency" hint="0 means unlimited.">
            <Input value={draft.max_concurrency} inputMode="numeric" onChange={(e) => setDraft({ ...draft, max_concurrency: e.target.value })} />
          </Field>
          <Field label="Token quota" hint="0 means unmetered. Counts input plus output tokens.">
            <Input value={draft.token_quota} inputMode="numeric" onChange={(e) => setDraft({ ...draft, token_quota: e.target.value })} />
          </Field>
          <Field label="Expires in days" hint="0 means the key never expires.">
            <Input value={draft.expires_in_days} inputMode="numeric" onChange={(e) => setDraft({ ...draft, expires_in_days: e.target.value })} />
          </Field>
          <div className="sm:col-span-2">
            <Field label="Allowed aliases" hint="Comma separated. Empty means every published alias.">
              <Input value={draft.allowed_aliases} onChange={(e) => setDraft({ ...draft, allowed_aliases: e.target.value })} placeholder="fast-chat, embed" autoComplete="off" />
            </Field>
          </div>
        </div>
      </Modal>

      <Modal
        open={minted !== null}
        title="Your new API key"
        onClose={() => setMinted(null)}
        footer={null}
      >
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
            The replacement inherits this key&apos;s limits. The current key keeps working during the
            grace window, then stops. The old secret is never shown again.
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
