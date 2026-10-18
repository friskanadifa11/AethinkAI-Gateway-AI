// UI primitives. Every page composes these instead of styling its own buttons,
// which is what keeps the console visually consistent as pages are added.
//
// Accessibility notes that are load-bearing:
//   - Button and Link both expose a visible focus ring via :focus-visible
//   - Modal traps Escape, restores focus, and marks the background inert
//   - Badge never encodes meaning in colour alone; it always carries a label

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
} from "react";

// ------------------------------------------------------------------ helpers

export function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(" ");
}

// ------------------------------------------------------------------ surface

export function Panel({
  title,
  subtitle,
  actions,
  children,
  className,
  as: Tag = "section",
}: {
  title?: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
  as?: "section" | "aside" | "div";
}) {
  return (
    <Tag className={cx("panel animate-rise-in", className)}>
      {(title || actions) && (
        <header className="flex flex-wrap items-start justify-between gap-3 border-b border-[color:var(--surface-border)] px-5 py-4">
          <div className="min-w-0">
            {title && (
              <h2 className="truncate text-[13px] font-semibold uppercase tracking-wider text-[color:var(--text-strong)]">
                {title}
              </h2>
            )}
            {subtitle && <p className="mt-1 text-xs muted">{subtitle}</p>}
          </div>
          {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
        </header>
      )}
      <div className="px-5 py-4">{children}</div>
    </Tag>
  );
}

// ------------------------------------------------------------------- button

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "ghost" | "danger" | "quiet";
  size?: "sm" | "md";
  busy?: boolean;
};

export function Button({
  variant = "ghost",
  size = "md",
  busy = false,
  className,
  children,
  disabled,
  ...rest
}: ButtonProps) {
  const base =
    "inline-flex items-center justify-center gap-2 rounded-[var(--radius-sm)] font-medium transition-[background,border-color,color] duration-[var(--dur-fast)] disabled:cursor-not-allowed disabled:opacity-50";
  const sizes = { sm: "px-2.5 py-1 text-xs", md: "px-3.5 py-2 text-sm" } as const;
  const variants = {
    primary:
      "bg-[color:var(--signal-strong)] text-[color:var(--surface-void)] hover:bg-[color:var(--signal)]",
    ghost:
      "border border-[color:var(--surface-border-strong)] bg-[color:var(--surface-raised)] text-[color:var(--text-body)] hover:bg-[color:var(--surface-hover)]",
    quiet: "text-[color:var(--text-muted)] hover:text-[color:var(--text-strong)] hover:bg-[color:var(--surface-hover)]",
    danger:
      "border border-[color:var(--bad)]/40 bg-transparent text-[color:var(--bad)] hover:bg-[color:var(--bad-dim)]",
  } as const;

  return (
    <button
      {...rest}
      disabled={disabled || busy}
      aria-busy={busy || undefined}
      className={cx(base, sizes[size], variants[variant], className)}
    >
      {busy && <Spinner size={12} />}
      {children}
    </button>
  );
}

export function Spinner({ size = 14 }: { size?: number }) {
  return (
    <span
      role="presentation"
      className="inline-block animate-spin rounded-full border-2 border-current border-t-transparent"
      style={{ width: size, height: size }}
    />
  );
}

// -------------------------------------------------------------------- badge

export function Badge({
  tone = "idle",
  children,
  pulse = false,
}: {
  tone?: "ok" | "warn" | "bad" | "idle" | "signal";
  children: ReactNode;
  pulse?: boolean;
}) {
  const tones = {
    ok: "border-[color:var(--ok)]/35 bg-[color:var(--ok-dim)] text-[color:var(--ok)]",
    warn: "border-[color:var(--warn)]/35 bg-[color:var(--warn-dim)] text-[color:var(--warn)]",
    bad: "border-[color:var(--bad)]/35 bg-[color:var(--bad-dim)] text-[color:var(--bad)]",
    signal: "border-[color:var(--signal)]/35 bg-[color:var(--signal-dim)] text-[color:var(--signal)]",
    idle: "border-[color:var(--surface-border-strong)] bg-[color:var(--surface-raised)] text-[color:var(--text-muted)]",
  } as const;
  return (
    <span
      className={cx(
        "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-medium",
        tones[tone],
      )}
    >
      <span
        aria-hidden="true"
        className={cx("h-1.5 w-1.5 rounded-full bg-current", pulse && "animate-pulse-rail")}
      />
      {children}
    </span>
  );
}

// -------------------------------------------------------------------- fields

export function Field({
  label,
  hint,
  error,
  children,
  htmlFor,
}: {
  label: string;
  hint?: string;
  error?: string;
  children: ReactNode;
  htmlFor?: string;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={htmlFor} className="text-xs font-medium text-[color:var(--text-muted)]">
        {label}
      </label>
      {children}
      {hint && !error && <p className="text-[11px] faint">{hint}</p>}
      {error && (
        <p role="alert" className="text-[11px] text-[color:var(--bad)]">
          {error}
        </p>
      )}
    </div>
  );
}

const controlClass =
  "w-full rounded-[var(--radius-sm)] border border-[color:var(--surface-border-strong)] bg-[color:var(--surface-void)] px-3 py-2 text-sm text-[color:var(--text-strong)] placeholder:text-[color:var(--text-faint)] focus:border-[color:var(--signal)] focus:outline-none";

export function Input({ className, ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...rest} className={cx(controlClass, className)} />;
}

export function Select({ className, children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select {...rest} className={cx(controlClass, "cursor-pointer", className)}>
      {children}
    </select>
  );
}

// --------------------------------------------------------------------- table

export function Table({ head, children }: { head: ReactNode[]; children: ReactNode }) {
  return (
    <div className="-mx-5 overflow-x-auto px-5">
      <table className="w-full border-collapse text-left text-sm">
        <thead>
          <tr className="border-b border-[color:var(--surface-border)]">
            {head.map((h, i) => (
              <th
                key={i}
                scope="col"
                className="whitespace-nowrap py-2 pr-4 text-[11px] font-semibold uppercase tracking-wider faint last:pr-0"
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  );
}

export function Row({ children, onClick }: { children: ReactNode; onClick?: () => void }) {
  return (
    <tr
      onClick={onClick}
      className={cx(
        "border-b border-[color:var(--surface-border)]/60 align-middle transition-colors duration-[var(--dur-fast)]",
        onClick && "cursor-pointer hover:bg-[color:var(--surface-hover)]",
      )}
    >
      {children}
    </tr>
  );
}

export function Cell({
  children,
  className,
  mono = false,
  title,
}: {
  children: ReactNode;
  className?: string;
  mono?: boolean;
  /** Native tooltip, used to expand a relative timestamp into an absolute one. */
  title?: string;
}) {
  return (
    <td title={title} className={cx("py-2.5 pr-4 last:pr-0", mono && "ident", className)}>
      {children}
    </td>
  );
}

// --------------------------------------------------------------- statements

export function EmptyState({
  title,
  body,
  action,
}: {
  title: string;
  body: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-start gap-3 rounded-[var(--radius)] border border-dashed border-[color:var(--surface-border-strong)] px-5 py-8">
      <p className="text-sm font-medium text-[color:var(--text-strong)]">{title}</p>
      <p className="max-w-prose text-xs muted">{body}</p>
      {action}
    </div>
  );
}

export function ErrorNote({ error, onRetry }: { error: Error | null; onRetry?: () => void }) {
  if (!error) return null;
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-[var(--radius)] border border-[color:var(--bad)]/35 bg-[color:var(--bad-dim)] px-4 py-3">
      <p className="text-xs text-[color:var(--bad)]">{error.message}</p>
      {onRetry && (
        <Button size="sm" variant="ghost" onClick={onRetry}>
          Try again
        </Button>
      )}
    </div>
  );
}

export function Loading({ label = "Loading" }: { label?: string }) {
  return (
    <div className="flex items-center gap-2 py-6 text-xs muted" role="status" aria-live="polite">
      <Spinner /> {label}…
    </div>
  );
}

// --------------------------------------------------------------------- modal

export function Modal({
  open,
  title,
  onClose,
  children,
  footer,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  const titleId = useId();
  const panelRef = useRef<HTMLDivElement>(null);
  const restoreTo = useRef<HTMLElement | null>(null);

  // Callers build a fresh onClose on every render, so it cannot be a dependency
  // of the effect below: the effect would tear down and re-run on each parent
  // render, and that re-run is what dragged the caret out of the field the
  // operator was typing in after every keystroke. The ref keeps Escape bound to
  // the current handler without the effect ever re-running.
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    if (!open) return;
    // Remember the trigger once, on open. A later run must not overwrite it
    // with whatever is focused now, or closing would return focus to the dialog.
    if (restoreTo.current === null) {
      restoreTo.current = document.activeElement as HTMLElement | null;
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onCloseRef.current();
    };
    document.addEventListener("keydown", onKey);

    // Land the caret in the first field rather than on the close button:
    // opening a form is an intent to fill it in. A dialog with no fields falls
    // back to a button so it stays reachable by keyboard.
    const panel = panelRef.current;
    const field = panel?.querySelector<HTMLElement>("input:not([type=hidden]),select,textarea");
    (field ?? panel?.querySelector<HTMLElement>("button"))?.focus();

    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  // Return focus only when the dialog actually closes, and only to a node that
  // is still on the page.
  useEffect(() => {
    if (open) return;
    const target = restoreTo.current;
    restoreTo.current = null;
    if (target && document.contains(target)) target.focus();
  }, [open]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-40 flex items-start justify-center overflow-y-auto bg-black/70 p-4 pt-[8vh]">
      <div className="absolute inset-0" onClick={onClose} aria-hidden="true" />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="panel relative z-10 w-full max-w-2xl animate-rise-in"
      >
        <header className="flex items-center justify-between gap-4 border-b border-[color:var(--surface-border)] px-5 py-3.5">
          <h2 id={titleId} className="text-sm font-semibold text-[color:var(--text-strong)]">
            {title}
          </h2>
          <Button size="sm" variant="quiet" onClick={onClose} aria-label="Close dialog">
            Close
          </Button>
        </header>
        <div className="px-5 py-4">{children}</div>
        {footer && (
          <footer className="flex flex-wrap items-center justify-end gap-2 border-t border-[color:var(--surface-border)] px-5 py-3.5">
            {footer}
          </footer>
        )}
      </div>
    </div>
  );
}

// -------------------------------------------------------------------- toasts

type Toast = { id: number; tone: "ok" | "bad" | "signal"; message: string };
type ToastContextValue = { push: (tone: Toast["tone"], message: string) => void };

const ToastContext = createContext<ToastContextValue>({ push: () => {} });

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<Toast[]>([]);
  const push = useCallback((tone: Toast["tone"], message: string) => {
    const id = Date.now() + Math.random();
    setItems((prev) => [...prev, { id, tone, message }]);
    window.setTimeout(() => setItems((prev) => prev.filter((t) => t.id !== id)), 6000);
  }, []);
  const value = useMemo(() => ({ push }), [push]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div
        aria-live="polite"
        aria-atomic="false"
        className="pointer-events-none fixed bottom-4 right-4 z-50 flex w-[min(92vw,380px)] flex-col gap-2"
      >
        {items.map((t) => (
          <div
            key={t.id}
            className={cx(
              "pointer-events-auto animate-rise-in rounded-[var(--radius)] border px-4 py-3 text-xs shadow-lg",
              t.tone === "ok" && "border-[color:var(--ok)]/35 bg-[color:var(--surface-raised)] text-[color:var(--ok)]",
              t.tone === "bad" && "border-[color:var(--bad)]/35 bg-[color:var(--surface-raised)] text-[color:var(--bad)]",
              t.tone === "signal" && "border-[color:var(--signal)]/35 bg-[color:var(--surface-raised)] text-[color:var(--signal)]",
            )}
          >
            {t.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastContextValue {
  return useContext(ToastContext);
}

// -------------------------------------------------------------- metric tile

export function Metric({
  label,
  value,
  hint,
  tone = "idle",
}: {
  label: string;
  value: string;
  hint?: string;
  tone?: "ok" | "bad" | "signal" | "idle";
}) {
  return (
    <div className="panel-raised px-4 py-3">
      <p className="text-[11px] font-medium uppercase tracking-wider faint">{label}</p>
      <p
        className={cx(
          "mt-1 text-2xl font-semibold tabular-nums",
          tone === "ok" && "text-[color:var(--ok)]",
          tone === "bad" && "text-[color:var(--bad)]",
          tone === "signal" && "text-[color:var(--text-strong)]",
          tone === "idle" && "text-[color:var(--text-strong)]",
        )}
      >
        {value}
      </p>
      {hint && <p className="mt-0.5 text-[11px] faint">{hint}</p>}
    </div>
  );
}

// ---------------------------------------------------------------- copy field

/** Renders a secret-bearing value once, with a copy affordance and a warning
 *  that it will not be shown again. */
export function OneTimeSecret({ token, onDone }: { token: string; onDone: () => void }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex flex-col gap-3">
      <p className="text-xs text-[color:var(--warn)]">
        Copy this key now. The gateway stores only a hash, so it cannot be shown again.
      </p>
      <code className="ident block break-all rounded-[var(--radius-sm)] border border-[color:var(--surface-border-strong)] bg-[color:var(--surface-void)] px-3 py-2.5">
        {token}
      </code>
      <div className="flex gap-2">
        <Button
          variant="primary"
          size="sm"
          onClick={() => {
            void navigator.clipboard?.writeText(token).then(() => setCopied(true));
          }}
        >
          {copied ? "Copied" : "Copy key"}
        </Button>
        <Button size="sm" onClick={onDone}>
          Done
        </Button>
      </div>
    </div>
  );
}
