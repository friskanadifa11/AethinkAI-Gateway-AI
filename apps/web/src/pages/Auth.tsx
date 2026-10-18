// Sign-in and first-run setup.
//
// The first-run path is gated by a one-use token that the operator reads from
// the server's .env, so a gateway that is reachable but not yet claimed cannot
// be taken over by whoever finds it first.

import { useState } from "react";
import { api, ApiError, type AdminUser, type SetupState } from "../lib/api";
import { MIN_PASSWORD_LENGTH } from "../lib/policy";
import { Button, Field, Input, Panel } from "../components/ui";
import { Logo } from "../components/AppShell";

export function AuthScreen({
  setup,
  onSignedIn,
  onRefreshSetup,
}: {
  setup: SetupState;
  onSignedIn: (user: AdminUser) => void;
  onRefreshSetup: () => void;
}) {
  const bootstrapping = setup.needs_bootstrap;

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    setError(null);
    if (bootstrapping && password !== confirm) {
      setError("The two passwords do not match.");
      return;
    }
    setBusy(true);
    try {
      const user = bootstrapping
        ? await api.bootstrap(username, password, token)
        : await api.login(username, password);
      onSignedIn(user);
    } catch (e) {
      if (e instanceof ApiError) {
        setError(e.message);
        // The installation may have been claimed in another tab.
        if (bootstrapping && e.status === 409) onRefreshSetup();
      } else {
        setError("The request could not be completed.");
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-[color:var(--surface-void)] px-4">
      <div className="w-full max-w-md">
        <div className="mb-5 flex items-center gap-3">
          <Logo size={28} />
          <div>
            <p className="text-sm font-semibold text-[color:var(--text-strong)]">
              AethinkAI Gateway AI
            </p>
            <p className="text-[11px] faint">Routing control plane</p>
          </div>
        </div>

        <Panel
          title={bootstrapping ? "Create the first operator" : "Sign in"}
          subtitle={
            bootstrapping
              ? "This installation has no operator yet. The token below is in the server's .env as BOOTSTRAP_TOKEN."
              : "Operator credentials are separate from client API keys."
          }
        >
          <form
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            <Field label="Username" htmlFor="auth-username">
              <Input
                id="auth-username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                autoFocus
              />
            </Field>

            <Field
              label="Password"
              htmlFor="auth-password"
              hint={
                bootstrapping
                  ? `At least ${MIN_PASSWORD_LENGTH} characters.`
                  : undefined
              }
            >
              <Input
                id="auth-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete={bootstrapping ? "new-password" : "current-password"}
              />
            </Field>

            {bootstrapping && (
              <>
                <Field label="Repeat password" htmlFor="auth-confirm">
                  <Input
                    id="auth-confirm"
                    type="password"
                    value={confirm}
                    onChange={(e) => setConfirm(e.target.value)}
                    autoComplete="new-password"
                  />
                </Field>
                <Field
                  label="Bootstrap token"
                  htmlFor="auth-token"
                  hint="From BOOTSTRAP_TOKEN in the gateway's environment."
                >
                  <Input
                    id="auth-token"
                    value={token}
                    onChange={(e) => setToken(e.target.value)}
                    autoComplete="off"
                  />
                </Field>
              </>
            )}

            {error && (
              <p role="alert" className="text-xs text-[color:var(--bad)]">
                {error}
              </p>
            )}

            <Button type="submit" variant="primary" busy={busy}>
              {bootstrapping ? "Create operator and sign in" : "Sign in"}
            </Button>

            {bootstrapping && !setup.bootstrap_configured && (
              <p className="text-xs text-[color:var(--warn)]">
                BOOTSTRAP_TOKEN is not set on the server, so first-run setup is closed. Set it and
                restart the gateway.
              </p>
            )}
          </form>
        </Panel>

        <p className="mt-4 text-center text-[11px] faint">
          Sessions are stored server-side and revoked on sign-out.
        </p>
      </div>
    </div>
  );
}
