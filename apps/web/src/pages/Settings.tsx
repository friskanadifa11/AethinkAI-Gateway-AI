// Settings: owner-editable operational values plus the operator's own account.
// Nothing here can weaken the security posture silently — the fields that would
// are either absent (no "disable auth") or require the current password.

import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type Settings as SettingsData } from "../lib/api";
import { relative } from "../lib/format";
import { MIN_PASSWORD_LENGTH } from "../lib/policy";
import {
  Badge,
  Button,
  Cell,
  EmptyState,
  ErrorNote,
  Field,
  Input,
  Loading,
  Panel,
  Row,
  Select,
  Table,
  useToast,
} from "../components/ui";

export function SettingsPage({ username }: { username: string }) {
  const toast = useToast();
  const [settings, setSettings] = useState<SettingsData | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [saving, setSaving] = useState(false);
  const [audit, setAudit] = useState<Array<{ actor: string; action: string; target: string; created_at: string }>>([]);

  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [changing, setChanging] = useState(false);

  const load = useCallback(async () => {
    try {
      const [set, events] = await Promise.all([api.getSettings(), api.audit(10)]);
      setSettings(set);
      setAudit(events);
      setError(null);
    } catch (e) {
      setError(e as Error);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const save = async () => {
    if (!settings) return;
    setSaving(true);
    try {
      const saved = await api.putSettings({
        site_name: settings.site_name,
        public_api_url: settings.public_api_url,
        timezone: settings.timezone,
        default_timeout_ms: settings.default_timeout_ms,
        retention_days: settings.retention_days,
        motion: settings.motion,
      });
      setSettings(saved);
      toast.push("ok", "Settings saved.");
      await load();
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "Settings could not be saved.");
    } finally {
      setSaving(false);
    }
  };

  const changePassword = async () => {
    if (next !== confirm) {
      toast.push("bad", "The two new passwords do not match.");
      return;
    }
    setChanging(true);
    try {
      await api.changePassword(current, next);
      setCurrent("");
      setNext("");
      setConfirm("");
      toast.push("ok", "Password changed. Other sessions were signed out.");
    } catch (e) {
      toast.push("bad", e instanceof ApiError ? e.message : "The password could not be changed.");
    } finally {
      setChanging(false);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <ErrorNote error={error} onRetry={() => void load()} />
      {settings === null && !error && <Loading label="Loading settings" />}

      {settings && (
        <>
          <Panel
            title="General"
            subtitle="Display and retention"
            actions={
              <Button variant="primary" busy={saving} onClick={() => void save()}>
                Save changes
              </Button>
            }
          >
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <Field label="Site name" hint="Shown in the console rail and page title.">
                <Input
                  value={settings.site_name}
                  onChange={(e) => setSettings({ ...settings, site_name: e.target.value })}
                />
              </Field>
              <Field label="Timezone" hint="IANA name, used when timestamps are localised.">
                <Input
                  value={settings.timezone}
                  onChange={(e) => setSettings({ ...settings, timezone: e.target.value })}
                  placeholder="UTC"
                />
              </Field>
              <Field label="Public API URL" hint="Shown to callers as the base URL. Optional.">
                <Input
                  value={settings.public_api_url}
                  onChange={(e) => setSettings({ ...settings, public_api_url: e.target.value })}
                  placeholder="https://gateway.example.com"
                />
              </Field>
              <Field label="Default timeout (ms)" hint="Used when a route target does not set its own.">
                <Input
                  value={String(settings.default_timeout_ms)}
                  inputMode="numeric"
                  onChange={(e) =>
                    setSettings({ ...settings, default_timeout_ms: Number(e.target.value) || 0 })
                  }
                />
              </Field>
              <Field label="Retention (days)" hint="Request metadata older than this is purged hourly.">
                <Input
                  value={String(settings.retention_days)}
                  inputMode="numeric"
                  onChange={(e) =>
                    setSettings({ ...settings, retention_days: Number(e.target.value) || 0 })
                  }
                />
              </Field>
              <Field label="Motion" hint="Reduced collapses every animation to a near-instant state.">
                <Select
                  value={settings.motion}
                  onChange={(e) => setSettings({ ...settings, motion: e.target.value })}
                >
                  <option value="system">Follow the operating system</option>
                  <option value="full">Always animate</option>
                  <option value="reduced">Always reduced</option>
                </Select>
              </Field>
            </div>
          </Panel>

          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            <Panel title="Operator account" subtitle={`Signed in as ${username}`}>
              <div className="flex flex-col gap-4">
                <Field label="Current password">
                  <Input
                    type="password"
                    value={current}
                    onChange={(e) => setCurrent(e.target.value)}
                    autoComplete="current-password"
                  />
                </Field>
                <Field
                  label="New password"
                  hint={`At least ${MIN_PASSWORD_LENGTH} characters.`}
                >
                  <Input
                    type="password"
                    value={next}
                    onChange={(e) => setNext(e.target.value)}
                    autoComplete="new-password"
                  />
                </Field>
                <Field label="Repeat new password">
                  <Input
                    type="password"
                    value={confirm}
                    onChange={(e) => setConfirm(e.target.value)}
                    autoComplete="new-password"
                  />
                </Field>
                <div className="flex items-center gap-3">
                  <Button
                    variant="primary"
                    busy={changing}
                    disabled={current === "" || next.length < MIN_PASSWORD_LENGTH}
                    onClick={() => void changePassword()}
                  >
                    Change password
                  </Button>
                  {next.length > 0 && next.length < MIN_PASSWORD_LENGTH && (
                    <span className="text-[11px] text-[color:var(--warn)]">
                      {MIN_PASSWORD_LENGTH - next.length} more character
                      {MIN_PASSWORD_LENGTH - next.length === 1 ? "" : "s"} needed
                    </span>
                  )}
                </div>
              </div>
            </Panel>

            <Panel
              title="Recent operator activity"
              subtitle="The newest ten audit entries"
              actions={<Badge tone="signal">audit</Badge>}
            >
              {audit.length === 0 ? (
                <EmptyState
                  title="No activity recorded"
                  body="Actions such as signing in or editing a route are recorded here."
                />
              ) : (
                <Table head={["Actor", "Action", "Target", "When"]}>
                  {audit.map((e, i) => (
                    <Row key={`${e.created_at}-${i}`}>
                      <Cell>{e.actor || "—"}</Cell>
                      <Cell mono>{e.action}</Cell>
                      <Cell mono>{e.target || "—"}</Cell>
                      <Cell className="muted">{relative(e.created_at)}</Cell>
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
