// The console frame: a fixed rail for navigation and a top bar for context.
//
// Why a rail rather than a top nav: routing is the product's central idea, and
// a persistent vertical list keeps the operator's position visible while they
// are three levels deep in a trace drawer.

import { useEffect, useState, type ReactNode } from "react";
import { Badge, Button, cx } from "../components/ui";
import { ROUTES, type RouteKey, pathFor } from "../app/router";

export function Logo({ size = 22 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" role="img" aria-label="AethinkAI Gateway AI">
      <defs>
        <linearGradient id="aethink-signal" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor="#7dd3fc" />
          <stop offset="100%" stopColor="#0ea5e9" />
        </linearGradient>
      </defs>
      <rect x="1" y="1" width="30" height="30" rx="8" fill="#0b1120" stroke="url(#aethink-signal)" />
      {/* Three routing lanes converging on one output node. */}
      <path d="M7 9h7l5 7" stroke="url(#aethink-signal)" strokeWidth="2" fill="none" strokeLinecap="round" />
      <path d="M7 16h7" stroke="url(#aethink-signal)" strokeWidth="2" fill="none" strokeLinecap="round" />
      <path d="M7 23h7l5-7" stroke="url(#aethink-signal)" strokeWidth="2" fill="none" strokeLinecap="round" />
      <circle cx="23" cy="16" r="3" fill="url(#aethink-signal)" />
    </svg>
  );
}

export function AppShell({
  route,
  navigate,
  user,
  siteName,
  onSignOut,
  onRefresh,
  refreshing,
  children,
}: {
  route: RouteKey;
  navigate: (key: RouteKey) => void;
  user?: { username: string } | null;
  siteName: string;
  onSignOut?: () => void;
  onRefresh?: () => void;
  refreshing?: boolean;
  children: ReactNode;
}) {
  const [motion, setMotion] = useState<"system" | "reduced" | "full">("system");
  const active = ROUTES.find((r) => r.key === route);

  useEffect(() => {
    const stored = localStorage.getItem("aethink.motion") as typeof motion | null;
    if (stored) setMotion(stored);
  }, []);

  const applyMotion = () => {
    const next = motion === "reduced" ? "full" : motion === "full" ? "system" : "reduced";
    setMotion(next);
    if (next === "system") {
      delete document.documentElement.dataset.motion;
      localStorage.removeItem("aethink.motion");
    } else {
      document.documentElement.dataset.motion = next;
      localStorage.setItem("aethink.motion", next);
    }
  };

  const groups: Array<{ label: string; key: string }> = [
    { label: "Traffic", key: "traffic" },
    { label: "Admin", key: "admin" },
  ];

  return (
    <div className="min-h-screen bg-[color:var(--surface-void)]">
      <a className="skip-link text-sm" href="#main">
        Skip to content
      </a>

      <div className="flex min-h-screen">
        <aside
          aria-label="Primary"
          className="hidden w-[var(--rail-width)] shrink-0 flex-col border-r border-[color:var(--surface-border)] bg-[color:var(--surface-rail)] lg:flex"
        >
          <div className="flex items-center gap-2.5 px-5 py-4">
            <Logo />
            <div className="min-w-0">
              <p className="truncate text-[13px] font-semibold text-[color:var(--text-strong)]">{siteName}</p>
              <p className="text-[10px] uppercase tracking-widest faint">Routing control plane</p>
            </div>
          </div>

          <nav className="flex flex-1 flex-col gap-5 px-3 py-3">
            {groups.map((group) => (
              <div key={group.key}>
                <p className="px-2 pb-1.5 text-[10px] font-semibold uppercase tracking-widest faint">
                  {group.label}
                </p>
                <ul className="flex flex-col gap-0.5">
                  {ROUTES.filter((r) => r.group === group.key).map((r) => {
                    const isActive = r.key === route;
                    return (
                      <li key={r.key}>
                        <a
                          href={pathFor(r.key)}
                          onClick={(e) => {
                            e.preventDefault();
                            navigate(r.key);
                          }}
                          aria-current={isActive ? "page" : undefined}
                          className={cx(
                            "flex items-center justify-between rounded-[var(--radius-sm)] px-2.5 py-2 text-[13px] transition-colors duration-[var(--dur-fast)]",
                            isActive
                              ? "signal-bar bg-[color:var(--surface-hover)] text-[color:var(--text-strong)]"
                              : "text-[color:var(--text-muted)] hover:bg-[color:var(--surface-hover)] hover:text-[color:var(--text-body)]",
                          )}
                        >
                          <span className="pl-2">{r.label}</span>
                        </a>
                      </li>
                    );
                  })}
                </ul>
              </div>
            ))}
          </nav>

          <div className="border-t border-[color:var(--surface-border)] px-4 py-3">
            <Button size="sm" variant="quiet" onClick={applyMotion} className="w-full justify-start">
              Motion: {motion}
            </Button>
            {user && (
              <div className="mt-2 flex items-center justify-between gap-2">
                <span className="truncate text-xs muted" title={user.username}>
                  {user.username}
                </span>
                {onSignOut && (
                  <Button size="sm" variant="quiet" onClick={onSignOut}>
                    Sign out
                  </Button>
                )}
              </div>
            )}
          </div>
        </aside>

        <div className="flex min-w-0 flex-1 flex-col">
          <header className="sticky top-0 z-20 flex h-[var(--topbar-height)] items-center gap-3 border-b border-[color:var(--surface-border)] bg-[color:var(--surface-rail)]/95 px-4 backdrop-blur lg:px-6">
            <span className="lg:hidden">
              <Logo size={20} />
            </span>
            <div className="min-w-0 flex-1">
              <h1
                id="page-title"
                tabIndex={-1}
                className="truncate text-sm font-semibold text-[color:var(--text-strong)] outline-none"
              >
                {active?.label ?? "Not found"}
              </h1>
              {active && <p className="truncate text-[11px] faint">{active.hint}</p>}
            </div>
            <div className="flex items-center gap-2">
              <Badge tone="signal" pulse>
                Gateway online
              </Badge>
              {onRefresh && (
                <Button size="sm" onClick={onRefresh} busy={refreshing} aria-label="Refresh this page">
                  Refresh
                </Button>
              )}
            </div>
          </header>

          {/* Small screens get the rail as a horizontal scroller. */}
          <nav
            aria-label="Primary (compact)"
            className="flex gap-1 overflow-x-auto border-b border-[color:var(--surface-border)] px-3 py-2 lg:hidden"
          >
            {ROUTES.map((r) => (
              <a
                key={r.key}
                href={pathFor(r.key)}
                onClick={(e) => {
                  e.preventDefault();
                  navigate(r.key);
                }}
                aria-current={r.key === route ? "page" : undefined}
                className={cx(
                  "whitespace-nowrap rounded-full px-3 py-1 text-xs",
                  r.key === route
                    ? "bg-[color:var(--signal-dim)] text-[color:var(--signal)]"
                    : "text-[color:var(--text-muted)]",
                )}
              >
                {r.label}
              </a>
            ))}
          </nav>

          <main id="main" className="flex-1 px-4 py-5 lg:px-6 lg:py-6">
            {children}
          </main>
        </div>
      </div>
    </div>
  );
}
