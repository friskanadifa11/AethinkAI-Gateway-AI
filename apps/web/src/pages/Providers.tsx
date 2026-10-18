// Providers: the upstream endpoints the gateway is allowed to call.
//
// The credential is write-only by design. The console never displays a stored
// credential; it shows whether one exists and lets the operator replace it.

import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type Provider } from "../lib/api";
import { tone } from "../lib/format";
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
  Table,
  useToast,
} from "../components/ui";

type Draft = {
  name: string;
  base_url: string;
  timeout_ms: string;
  api_key: string;
  enabled: boolean;
  models: string;
};

const EMPTY: Draft = {
  name: "",
  base_url: "https://api.openai.com/v1",
  timeout_ms: "60000",
  api_key: "",
  enabled: true,
  models: "",
};

export function ProvidersPage() {
  const toast = useToast();
  const [items, setItems] = useState<Provider[] | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [editing, setEditing] = useState<Provider | null>(null);
  const [creating, setCreating] = useState(false);
  const [draft, setDraft] = useState<Draft>(EMPTY);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<Provider | null>(null);

  const load = useCallback(async () => {
    try {
      setItems(await api.listProviders());
      setError(null);
    } catch (e) {
      setError(e as Error);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const openCreate = () => {
    setDraft(EMPTY);
    setEditing(null);
    setCreating(true);
  };

  const openEdit = (p: Provider) => {
    setDraft({
      name: p.name,
      base_url: p.base_url.startsWith("http") ? p.base_url : `https://${p.base_url}`,
      timeout_ms: String(p.timeout_ms),
      api_key: "",
      enabled: p.enabled,
      models: p.models.join(", "),
    });
    setEditing(p);
    setCreating(false);
  };

  const closeForm = () => {
    setCreating(false);
    setEditing(null);
  };

  const submit = async () => {
    setSaving(true);
    const payload = {
      name: draft.name.trim(),
      base_url: draft.base_url.trim(),
      timeout_ms: Number(draft.timeout_ms) || 60000,
      api_key: draft.api_key,
      enabled: draft.enabled,
      adapter: "openai-compatible",
      models: draft.models
        .split(",")
        .map((m) => m.trim())
        .filter(Boolean),
    };
    try {
      if (editing) {
        await api.updateProvider(editing.id, payload);
        toast.push("ok", `${payload.name} updated.`);
      } else {
        await api.createProvider(payload);
        toast.push("ok", `${payload.name} added.`);
      }
      closeForm();
      await load();
    } catch (e) {
      const message = e instanceof ApiError ? e.message : "The provider could not be saved.";
      toast.push("bad", message);
    } finally {
      setSaving(false);
    }
  };

  const runTest = async (p: Provider) => {
    setTesting(p.id);
    try {
      const result = await api.testProvider(p.id);
      if (result.state === "healthy") {
        toast.push("ok", `${p.name} answered the connection check.`);
      } else {
        toast.push("bad", result.error || `${p.name} did not answer.`);
      }
      await load();
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "The check could not be run.");
    } finally {
      setTesting(null);
    }
  };

  const remove = async (p: Provider) => {
    try {
      await api.deleteProvider(p.id);
      toast.push("ok", `${p.name} removed.`);
      setConfirmDelete(null);
      await load();
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "The provider could not be removed.");
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-prose text-xs muted">
          Each provider is one OpenAI-compatible base URL. Credentials are encrypted with AES-256-GCM
          before they are stored and are never returned to this console.
        </p>
        <Button variant="primary" onClick={openCreate}>
          Add provider
        </Button>
      </div>

      <ErrorNote error={error} onRetry={() => void load()} />

      <Panel title="Configured providers" subtitle="Including disabled entries">
        {items === null && !error ? (
          <Loading label="Loading providers" />
        ) : items && items.length === 0 ? (
          <EmptyState
            title="No providers yet"
            body="A route needs at least one provider target. Add one, then create a route on the Routing page."
            action={
              <Button variant="primary" size="sm" onClick={openCreate}>
                Add the first provider
              </Button>
            }
          />
        ) : (
          <Table head={["Name", "Host", "Adapter", "Timeout", "Credential", "State", "Models", "Actions"]}>
            {items?.map((p) => (
              <Row key={p.id}>
                <Cell>
                  <span className="font-medium text-[color:var(--text-strong)]">{p.name}</span>
                  {!p.enabled && (
                    <span className="ml-2 text-[10px] uppercase tracking-wide faint">disabled</span>
                  )}
                </Cell>
                <Cell mono>{p.base_url}</Cell>
                <Cell mono>{p.adapter}</Cell>
                <Cell mono>{p.timeout_ms} ms</Cell>
                <Cell>
                  {p.credential_set ? (
                    <Badge tone="signal">stored</Badge>
                  ) : (
                    <Badge tone="idle">none</Badge>
                  )}
                </Cell>
                <Cell>
                  <Badge tone={tone(p.last_test_state)}>{p.last_test_state}</Badge>
                </Cell>
                <Cell mono>{p.models.length ? p.models.length : "—"}</Cell>
                <Cell>
                  <div className="flex gap-1.5">
                    <Button size="sm" busy={testing === p.id} onClick={() => void runTest(p)}>
                      Test
                    </Button>
                    <Button size="sm" onClick={() => openEdit(p)}>
                      Edit
                    </Button>
                    <Button size="sm" variant="danger" onClick={() => setConfirmDelete(p)}>
                      Remove
                    </Button>
                  </div>
                </Cell>
              </Row>
            ))}
          </Table>
        )}
      </Panel>

      <Modal
        open={creating || editing !== null}
        title={editing ? `Edit ${editing.name}` : "Add provider"}
        onClose={closeForm}
        footer={
          <>
            <Button onClick={closeForm}>Cancel</Button>
            <Button variant="primary" busy={saving} onClick={() => void submit()}>
              {editing ? "Save changes" : "Add provider"}
            </Button>
          </>
        }
      >
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field
            label="Name"
            hint="Shown in traces and route targets."
            
          >
            <Input
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              placeholder="OpenAI primary"
              autoComplete="off"
            />
          </Field>
          <Field label="Timeout (ms)" hint="Applies when a route target does not override it.">
            <Input
              value={draft.timeout_ms}
              inputMode="numeric"
              onChange={(e) => setDraft({ ...draft, timeout_ms: e.target.value })}
            />
          </Field>
          <div className="sm:col-span-2">
            <Field
              label="Base URL"
              hint="The gateway appends /chat/completions. A URL ending in /v1 is kept as-is."
            >
              <Input
                value={draft.base_url}
                onChange={(e) => setDraft({ ...draft, base_url: e.target.value })}
                placeholder="https://api.example.com/v1"
                autoComplete="off"
              />
            </Field>
          </div>
          <div className="sm:col-span-2">
            <Field
              label={editing ? "Replace API key" : "API key"}
              hint={
                editing
                  ? "Leave empty to keep the stored credential. Typing a value replaces it permanently."
                  : "Encrypted at rest. It is never shown again after saving."
              }
            >
              <Input
                type="password"
                value={draft.api_key}
                onChange={(e) => setDraft({ ...draft, api_key: e.target.value })}
                placeholder={editing ? "•••••••• (unchanged)" : "sk-…"}
                autoComplete="new-password"
              />
            </Field>
          </div>
          <div className="sm:col-span-2">
            <Field label="Models" hint="Comma separated. Used by the console for hints; the route target decides the real model.">
              <Input
                value={draft.models}
                onChange={(e) => setDraft({ ...draft, models: e.target.value })}
                placeholder="gpt-4o-mini, gpt-4o"
                autoComplete="off"
              />
            </Field>
          </div>
          <label className="flex items-center gap-2 text-xs muted sm:col-span-2">
            <input
              type="checkbox"
              checked={draft.enabled}
              onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })}
              className="h-4 w-4 accent-[color:var(--signal-strong)]"
            />
            Enabled — a disabled provider is skipped by the router even if a route targets it.
          </label>
        </div>
      </Modal>

      <Modal
        open={confirmDelete !== null}
        title="Remove provider"
        onClose={() => setConfirmDelete(null)}
        footer={
          <>
            <Button onClick={() => setConfirmDelete(null)}>Keep it</Button>
            <Button variant="danger" onClick={() => confirmDelete && void remove(confirmDelete)}>
              Remove provider
            </Button>
          </>
        }
      >
        <p className="text-sm muted">
          {confirmDelete?.name} will be removed. A provider still referenced by a route version
          cannot be deleted; replace it in that route first.
        </p>
      </Modal>
    </div>
  );
}
