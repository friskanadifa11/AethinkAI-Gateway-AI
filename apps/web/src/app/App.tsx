// Application root: resolve the session, then render the shell around the page
// the URL asks for.
//
// Two rules the rest of the console relies on:
//   - an expired session drops the operator back to the sign-in screen instead
//     of rendering empty tables
//   - the Status page stays reachable without a session, because "is it up?"
//     must be answerable when sign-in itself is the problem

import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type AdminUser, type Settings, type SetupState } from "../lib/api";
import { PUBLIC_ROUTES, useRouter } from "./router";
import { AppShell } from "../components/AppShell";
import { ErrorNote, Panel, ToastProvider, Loading } from "../components/ui";
import { AuthScreen } from "../pages/Auth";
import { OverviewPage } from "../pages/Overview";
import { RoutingPage } from "../pages/Routing";
import { ProvidersPage } from "../pages/Providers";
import { ApiKeysPage } from "../pages/ApiKeys";
import { RequestsPage } from "../pages/Requests";
import { AuditPage } from "../pages/Audit";
import { StatusPage } from "../pages/Status";
import { DocsPage } from "../pages/Docs";
import { SettingsPage } from "../pages/Settings";

function Console() {
  const { route, navigate, notFound } = useRouter();
  const [user, setUser] = useState<AdminUser | null>(null);
  const [setup, setSetup] = useState<SetupState | null>(null);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [sessionChecked, setSessionChecked] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [focusTrace, setFocusTrace] = useState<string | undefined>(undefined);

  const loadSession = useCallback(async () => {
    try {
      setUser(await api.me());
    } catch (e) {
      // 401 is an ordinary state here, not a failure worth surfacing.
      if (!(e instanceof ApiError && e.isAuthExpired)) setError(e as Error);
      setUser(null);
    } finally {
      setSessionChecked(true);
    }
  }, []);

  const loadSetup = useCallback(async () => {
    try {
      setSetup(await api.setupState());
    } catch (e) {
      setError(e as Error);
    }
  }, []);

  useEffect(() => {
    void loadSession();
    void loadSetup();
  }, [loadSession, loadSetup]);

  const loadSettings = useCallback(async () => {
    try {
      setSettings(await api.getSettings());
    } catch {
      // Settings are cosmetic for the shell; a failure must not block the console.
      setSettings(null);
    }
  }, []);

  useEffect(() => {
    if (user) void loadSettings();
  }, [user, loadSettings]);

  const signOut = async () => {
    try {
      await api.logout();
    } finally {
      setUser(null);
      navigate("overview");
    }
  };

  const siteName = settings?.site_name ?? "AethinkAI Gateway AI";
  const publicOnly = PUBLIC_ROUTES.includes(route);

  if (!sessionChecked || setup === null) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Loading label="Starting the console" />
      </div>
    );
  }

  if (!user && !publicOnly) {
    return (
      <AuthScreen
        setup={setup}
        onSignedIn={(u) => {
          setUser(u);
          void loadSettings();
          navigate("overview");
        }}
        onRefreshSetup={() => void loadSetup()}
      />
    );
  }

  const openTrace = (traceId: string) => {
    setFocusTrace(traceId);
    navigate("requests");
  };

  return (
    <AppShell
      route={route}
      navigate={navigate}
      user={user}
      siteName={siteName}
      onSignOut={user ? () => void signOut() : undefined}
      onRefresh={() => {
        // Re-render the active page by nudging the router to the same route;
        // pages own their own fetch and will re-run on remount.
        navigate(route);
        void loadSettings();
      }}
    >
      {notFound && (
        <Panel title="Unknown page">
          <p className="text-xs muted">
            That path is not part of the console. Use the navigation to pick a page.
          </p>
        </Panel>
      )}
      {error && <ErrorNote error={error} onRetry={() => void loadSession()} />}

      {route === "overview" &&
        (user ? (
          <OverviewPage onOpenTrace={openTrace} />
        ) : (
          <SignInPrompt navigateOverview={() => navigate("overview")} />
        ))}
      {route === "routing" && user && <RoutingPage />}
      {route === "providers" && user && <ProvidersPage />}
      {route === "keys" && user && <ApiKeysPage />}
      {route === "requests" && user && (
        <RequestsPage focusTrace={focusTrace} onFocusHandled={() => setFocusTrace(undefined)} />
      )}
      {route === "audit" && user && <AuditPage />}
      {route === "status" && <StatusPage />}
      {route === "docs" && <DocsPage />}
      {route === "settings" && user && <SettingsPage username={user.username} />}
    </AppShell>
  );
}

function SignInPrompt({ navigateOverview }: { navigateOverview: () => void }) {
  return (
    <Panel title="Not signed in">
      <div className="flex flex-col gap-3">
        <p className="text-xs muted">
          This page needs an operator session. Reload to reach the sign-in screen.
        </p>
        <button className="self-start text-xs" onClick={navigateOverview}>
          Try again
        </button>
      </div>
    </Panel>
  );
}

export function App() {
  return (
    <ToastProvider>
      <Console />
    </ToastProvider>
  );
}
