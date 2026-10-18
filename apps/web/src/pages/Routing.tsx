// Routing: the product's centre. An alias is a stable name callers use; every
// change creates a new immutable version that must be published explicitly, so
// editing a live route never changes behaviour by accident.

import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type Provider, type Route, type RouteTarget } from "../lib/api";
import { relative } from "../lib/format";
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

type TargetDraft = {
  provider_id: string;
  upstream_model: string;
  timeout_ms: string;
  retries: string;
  enabled: boolean;
};

const blankTarget = (providerId: string): TargetDraft => ({
  provider_id: providerId,
  upstream_model: "",
  timeout_ms: "60000",
  retries: "0",
  enabled: true,
});

type DryRun = Awaited<ReturnType<typeof api.dryRun>>;

export function RoutingPage() {
  const toast = useToast();
  const [published, setPublished] = useState<Route[]>([]);
  const [drafts, setDrafts] = useState<Route[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  const [composing, setComposing] = useState(false);
  const [alias, setAlias] = useState("");
  const [targets, setTargets] = useState<TargetDraft[]>([]);
  const [saving, setSaving] = useState(false);

  const [preview, setPreview] = useState<{ route: Route; plan: DryRun } | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [routes, providerList] = await Promise.all([api.listRoutes(), api.listProviders()]);
      setPublished(routes.published);
      setDrafts(routes.drafts);
      setProviders(providerList);
      setError(null);
    } catch (e) {
      setError(e as Error);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const startCompose = (from?: Route) => {
    setAlias(from?.alias ?? "");
    setTargets(
      from && from.targets.length > 0
        ? from.targets.map((t) => ({
            provider_id: t.provider_id,
            upstream_model: t.upstream_model,
            timeout_ms: String(t.timeout_ms),
            retries: String(t.retries),
            enabled: t.enabled,
          }))
        : providers.length > 0
          ? [blankTarget(providers[0]!.id)]
          : [],
    );
    setComposing(true);
  };

  const saveDraft = async () => {
    setSaving(true);
    try {
      await api.createRoute({
        alias: alias.trim(),
        strategy: "priority",
        targets: targets.map((t) => ({
          provider_id: t.provider_id,
          upstream_model: t.upstream_model.trim(),
          timeout_ms: Number(t.timeout_ms) || 60000,
          retries: Number(t.retries) || 0,
          enabled: t.enabled,
        })),
      });
      toast.push("ok", `Draft version created for ${alias.trim()}.`);
      setComposing(false);
      await load();
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "The version could not be saved.");
    } finally {
      setSaving(false);
    }
  };

  const publish = async (route: Route) => {
    try {
      await api.publishRoute(route.id);
      toast.push("ok", `${route.alias} v${route.version} is now live.`);
      await load();
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "The version could not be published.");
    }
  };

  const removeDraft = async (route: Route) => {
    try {
      await api.deleteRoute(route.id);
      toast.push("ok", "Draft discarded.");
      await load();
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "The draft could not be discarded.");
    }
  };

  const dryRun = async (route: Route) => {
    try {
      setPreview({ route, plan: await api.dryRun(route.id) });
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "The preview could not be produced.");
    }
  };

  const providerName = (id: string) => providers.find((p) => p.id === id)?.name ?? id.slice(0, 8);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-prose text-xs muted">
          The router tries targets in position order: position 0 first, then each fallback. A
          published version is immutable — change a route by publishing the next version.
        </p>
        <Button variant="primary" onClick={() => startCompose()} disabled={providers.length === 0}>
          New route version
        </Button>
      </div>

      <ErrorNote error={error} onRetry={() => void load()} />
      {loading && published.length === 0 && drafts.length === 0 && <Loading label="Loading routes" />}

      {!loading && providers.length === 0 && (
        <EmptyState
          title="Add a provider first"
          body="A route target points at a provider. Configure at least one provider, then define the route."
        />
      )}

      <Panel title="Live" subtitle="The version serving traffic right now">
        {published.length === 0 ? (
          <EmptyState
            title="No published routes"
            body="No alias is currently answering. Calls to /v1/chat/completions will return model_not_found until one is published."
          />
        ) : (
          <div className="flex flex-col gap-3">
            {published.map((r) => (
              <RouteCard
                key={r.id}
                route={r}
                providerName={providerName}
                onDryRun={() => void dryRun(r)}
                onClone={() => startCompose(r)}
              />
            ))}
          </div>
        )}
      </Panel>

      <Panel title="Drafts" subtitle="Saved versions that are not serving traffic">
        {drafts.length === 0 ? (
          <EmptyState
            title="No drafts"
            body="Versions you save land here until you publish them, so a live route is never edited in place."
          />
        ) : (
          <Table head={["Alias", "Version", "Targets", "Created", "Actions"]}>
            {drafts.map((r) => (
              <Row key={r.id}>
                <Cell mono>{r.alias}</Cell>
                <Cell mono>v{r.version}</Cell>
                <Cell mono>{r.targets.length}</Cell>
                <Cell className="muted">{relative(r.created_at)}</Cell>
                <Cell>
                  <div className="flex gap-1.5">
                    <Button size="sm" variant="primary" onClick={() => void publish(r)}>
                      Publish
                    </Button>
                    <Button size="sm" onClick={() => void dryRun(r)}>
                      Preview
                    </Button>
                    <Button size="sm" variant="danger" onClick={() => void removeDraft(r)}>
                      Discard
                    </Button>
                  </div>
                </Cell>
              </Row>
            ))}
          </Table>
        )}
      </Panel>

      {/* ---------------------------------------------------------- compose */}
      <Modal
        open={composing}
        title="New route version"
        onClose={() => setComposing(false)}
        footer={
          <>
            <Button onClick={() => setComposing(false)}>Cancel</Button>
            <Button
              variant="primary"
              busy={saving}
              onClick={() => void saveDraft()}
              disabled={alias.trim() === "" || targets.length === 0}
            >
              Save draft
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-4">
          <Field label="Alias" hint="Lowercase letters, digits, dot, dash or underscore. This is the value callers pass as model.">
            <Input
              value={alias}
              onChange={(e) => setAlias(e.target.value)}
              placeholder="fast-chat"
              autoComplete="off"
            />
          </Field>

          <div className="flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <p className="text-xs font-medium text-[color:var(--text-muted)]">
                Targets, in fallback order
              </p>
              <Button
                size="sm"
                onClick={() =>
                  setTargets([
                    ...targets,
                    blankTarget(providers[0]?.id ?? ""),
                  ])
                }
                disabled={providers.length === 0 || targets.length >= 10}
              >
                Add target
              </Button>
            </div>

            {targets.map((t, index) => (
              <div
                key={index}
                className="panel-raised grid grid-cols-1 gap-3 px-3 py-3 sm:grid-cols-[auto_1fr_1fr_auto_auto] sm:items-end"
              >
                <span className="flex h-6 w-6 items-center justify-center rounded-full bg-[color:var(--signal-dim)] text-[11px] font-semibold text-[color:var(--signal)]">
                  {index}
                </span>
                <Field label="Provider">
                  <Select
                    value={t.provider_id}
                    onChange={(e) => {
                      const next = [...targets];
                      next[index] = { ...t, provider_id: e.target.value };
                      setTargets(next);
                    }}
                  >
                    {providers.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                        {p.enabled ? "" : " (disabled)"}
                      </option>
                    ))}
                  </Select>
                </Field>
                <Field label="Upstream model">
                  <Input
                    value={t.upstream_model}
                    onChange={(e) => {
                      const next = [...targets];
                      next[index] = { ...t, upstream_model: e.target.value };
                      setTargets(next);
                    }}
                    placeholder="gpt-4o-mini"
                    autoComplete="off"
                  />
                </Field>
                <Field label="Timeout">
                  <Input
                    className="w-24"
                    value={t.timeout_ms}
                    inputMode="numeric"
                    onChange={(e) => {
                      const next = [...targets];
                      next[index] = { ...t, timeout_ms: e.target.value };
                      setTargets(next);
                    }}
                  />
                </Field>
                <div className="flex items-center gap-2 pb-2">
                  <Field label="Retries">
                    <Input
                      className="w-16"
                      value={t.retries}
                      inputMode="numeric"
                      onChange={(e) => {
                        const next = [...targets];
                        next[index] = { ...t, retries: e.target.value };
                        setTargets(next);
                      }}
                    />
                  </Field>
                  <Button
                    size="sm"
                    variant="danger"
                    aria-label={`Remove target ${index}`}
                    onClick={() => setTargets(targets.filter((_, i) => i !== index))}
                  >
                    ×
                  </Button>
                </div>
              </div>
            ))}

            {targets.length === 0 && (
              <p className="text-xs text-[color:var(--bad)]">
                A route needs at least one target.
              </p>
            )}
          </div>
        </div>
      </Modal>

      {/* ---------------------------------------------------------- preview */}
      <Modal
        open={preview !== null}
        title={preview ? `Routing preview — ${preview.route.alias} v${preview.route.version}` : ""}
        onClose={() => setPreview(null)}
        footer={<Button onClick={() => setPreview(null)}>Close</Button>}
      >
        {preview && (
          <div className="flex flex-col gap-4">
            <p className="text-xs muted">
              This preview is computed from stored configuration only. No provider was contacted.
            </p>

            {preview.plan.would_fail ? (
              <div className="rounded-[var(--radius)] border border-[color:var(--bad)]/35 bg-[color:var(--bad-dim)] px-4 py-3 text-xs text-[color:var(--bad)]">
                Every target would be skipped. Publishing this version would answer every request
                with <span className="ident">no_healthy_target</span>.
              </div>
            ) : (
              <ol className="flex flex-col gap-2">
                {preview.plan.would_use.map((t) => (
                  <li
                    key={t.order}
                    className={cx(
                      "panel-raised flex items-center justify-between gap-3 px-3 py-2.5",
                      t.order === 0 && "signal-bar pl-4",
                    )}
                  >
                    <span className="flex items-center gap-3">
                      <span className="text-[11px] font-semibold faint">{t.order === 0 ? "primary" : `fallback ${t.order}`}</span>
                      <span className="text-sm text-[color:var(--text-strong)]">{t.provider_name}</span>
                      <span className="ident">{t.upstream_model}</span>
                    </span>
                    <span className="text-[11px] muted">
                      {t.timeout_s}s · {t.retries} retr{t.retries === 1 ? "y" : "ies"}
                    </span>
                  </li>
                ))}
              </ol>
            )}

            {preview.plan.skipped.length > 0 && (
              <div>
                <p className="mb-1.5 text-xs font-medium text-[color:var(--warn)]">Skipped targets</p>
                <ul className="flex flex-col gap-1">
                  {preview.plan.skipped.map((s, i) => (
                    <li key={i} className="text-xs muted">
                      {s.provider_name ?? s.provider_id.slice(0, 8)} — {s.reason}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
}

function RouteCard({
  route,
  providerName,
  onDryRun,
  onClone,
}: {
  route: Route;
  providerName: (id: string) => string;
  onDryRun: () => void;
  onClone: () => void;
}) {
  return (
    <article className="panel-raised px-4 py-3">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <span className="ident text-[13px] font-semibold text-[color:var(--text-strong)]">{route.alias}</span>
          <Badge tone="ok" pulse>
            live v{route.version}
          </Badge>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-[11px] faint">published {relative(route.published_at)}</span>
          <Button size="sm" onClick={onDryRun}>
            Preview
          </Button>
          <Button size="sm" onClick={onClone}>
            New version
          </Button>
        </div>
      </header>

      <ol className="mt-3 flex flex-col gap-1.5">
        {route.targets.map((t: RouteTarget) => (
          <li key={`${t.position}-${t.provider_id}`} className="flex flex-wrap items-center gap-2.5 text-xs">
            <span className="w-16 shrink-0 text-[10px] uppercase tracking-wide faint">
              {t.position === 0 ? "primary" : `fallback`}
            </span>
            <span className="text-[color:var(--text-body)]">{providerName(t.provider_id)}</span>
            <span className="ident">{t.upstream_model}</span>
            <span className="faint">{t.timeout_ms} ms</span>
            {t.retries > 0 && <span className="faint">{t.retries} retry</span>}
            {!t.enabled && <Badge tone="warn">disabled</Badge>}
          </li>
        ))}
      </ol>
    </article>
  );
}
