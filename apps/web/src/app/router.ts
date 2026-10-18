// A deliberately small router.
//
// The console is served by the gateway under /app/ and has nine fixed
// destinations, so a routing library would add a dependency without adding
// capability. Path is the source of truth; navigation uses pushState so the
// back button and deep links behave as expected.

import { useCallback, useEffect, useState } from "react";

export const BASE = "/app/";

export type RouteKey =
  | "overview"
  | "routing"
  | "providers"
  | "keys"
  | "requests"
  | "audit"
  | "status"
  | "docs"
  | "settings";

export type RouteDef = {
  key: RouteKey;
  path: string;
  label: string;
  /** Shown under the page title and in the rail's tooltip. */
  hint: string;
  group: "traffic" | "admin";
};

export const ROUTES: RouteDef[] = [
  { key: "overview", path: "overview", label: "Overview", hint: "Live traffic, latency and failures", group: "traffic" },
  { key: "routing", path: "routing", label: "Routing", hint: "Aliases, priority order and fallbacks", group: "traffic" },
  { key: "providers", path: "providers", label: "Providers", hint: "Upstream endpoints and credentials", group: "traffic" },
  { key: "requests", path: "requests", label: "Requests", hint: "Per-request traces and attempts", group: "traffic" },
  { key: "keys", path: "keys", label: "API keys", hint: "Client keys, limits and rotation", group: "admin" },
  { key: "status", path: "status", label: "Status", hint: "Liveness and readiness", group: "admin" },
  { key: "audit", path: "audit", label: "Audit log", hint: "Operator actions", group: "admin" },
  { key: "docs", path: "docs", label: "API reference", hint: "Public gateway contract", group: "admin" },
  { key: "settings", path: "settings", label: "Settings", hint: "Site, retention and security", group: "admin" },
];

/** Routes that a signed-out operator may still reach. */
export const PUBLIC_ROUTES: RouteKey[] = ["status"];

export function pathFor(key: RouteKey): string {
  const def = ROUTES.find((r) => r.key === key);
  return `${BASE}${def ? def.path : ""}`;
}

function parse(pathname: string): RouteKey {
  const rest = pathname.startsWith(BASE) ? pathname.slice(BASE.length) : pathname.slice(1);
  const segment = rest.split("/")[0] ?? "";
  const match = ROUTES.find((r) => r.path === segment);
  return match ? match.key : "overview";
}

export function useRouter(): {
  route: RouteKey;
  navigate: (key: RouteKey) => void;
  notFound: boolean;
} {
  const [route, setRoute] = useState<RouteKey>(() => parse(window.location.pathname));
  const [notFound, setNotFound] = useState(() => {
    const rest = window.location.pathname.startsWith(BASE)
      ? window.location.pathname.slice(BASE.length)
      : window.location.pathname.slice(1);
    const segment = rest.split("/")[0] ?? "";
    return segment !== "" && !ROUTES.some((r) => r.path === segment);
  });

  useEffect(() => {
    const onPop = () => {
      setRoute(parse(window.location.pathname));
      setNotFound(false);
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  const navigate = useCallback((key: RouteKey) => {
    const target = pathFor(key);
    if (window.location.pathname !== target) {
      window.history.pushState({}, "", target);
    }
    setRoute(key);
    setNotFound(false);
    // Move focus to the page heading so screen readers announce the change.
    window.requestAnimationFrame(() => {
      document.getElementById("page-title")?.focus();
    });
  }, []);

  return { route, navigate, notFound };
}
