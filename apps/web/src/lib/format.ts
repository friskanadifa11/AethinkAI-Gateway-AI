// Pure formatting helpers. Kept dependency-free and side-effect-free so they
// are directly testable and reusable from any page.

const nf = new Intl.NumberFormat("en-US");

/** Compact count: 1.2k, 3.4M. Used on metric tiles where width is tight. */
export function compact(n: number | undefined | null): string {
  if (n === undefined || n === null || Number.isNaN(n)) return "—";
  if (Math.abs(n) < 1000) return String(n);
  return new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 }).format(n);
}

/** Full grouped integer, for tables where precision matters. */
export function grouped(n: number | undefined | null): string {
  if (n === undefined || n === null || Number.isNaN(n)) return "—";
  return nf.format(n);
}

/** Latency in milliseconds, rendered in seconds once it passes 1s. */
export function ms(value: number | undefined | null): string {
  if (value === undefined || value === null) return "—";
  if (value < 1000) return `${Math.round(value)} ms`;
  return `${(value / 1000).toFixed(2)} s`;
}

/** Percentage with one decimal, guarding division by zero. */
export function pct(part: number, whole: number): string {
  if (!whole) return "—";
  return `${((part / whole) * 100).toFixed(1)}%`;
}

/** Relative time: "just now", "4m ago", "3d ago". */
export function relative(iso: string | undefined | null, now: Date = new Date()): string {
  if (!iso) return "—";
  const then = new Date(iso);
  if (Number.isNaN(then.getTime())) return "—";
  const seconds = Math.max(0, Math.floor((now.getTime() - then.getTime()) / 1000));
  if (seconds < 45) return "just now";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

/** Absolute timestamp in a stable, sortable form. */
export function stamp(iso: string | undefined | null): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toISOString().replace("T", " ").replace(".000Z", "Z").slice(0, 19) + "Z";
}

/** A key prefix is shown, never the key itself. */
export function keyPrefix(prefix: string | undefined): string {
  if (!prefix) return "—";
  return `${prefix}••••`;
}

/** Status → the tone used by badges and dots. */
export function tone(status: string | undefined): "ok" | "warn" | "bad" | "idle" {
  switch (status) {
    case "ok":
    case "healthy":
      return "ok";
    case "rejected":
      return "warn";
    case "error":
    case "untested":
      return status === "untested" ? "idle" : "bad";
    default:
      return "idle";
  }
}
