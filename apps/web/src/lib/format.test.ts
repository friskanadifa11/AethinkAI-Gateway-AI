import { describe, expect, it } from "vitest";
import { compact, grouped, keyPrefix, ms, pct, relative, stamp, tone } from "./format";

// Behaviour contracts, not snapshots: each case states what the operator must
// see, so a refactor that keeps the contract still passes.

describe("compact", () => {
  it("leaves small numbers alone", () => {
    expect(compact(42)).toBe("42");
    expect(compact(999)).toBe("999");
  });
  it("abbreviates large numbers", () => {
    expect(compact(1200)).toBe("1.2K");
    expect(compact(3_400_000)).toBe("3.4M");
  });
  it("never prints NaN to the operator", () => {
    expect(compact(null)).toBe("—");
    expect(compact(undefined)).toBe("—");
  });
});

describe("grouped", () => {
  it("groups thousands for table reading", () => {
    expect(grouped(1234567)).toBe("1,234,567");
  });
  it("renders zero as zero, not as a blank", () => {
    expect(grouped(0)).toBe("0");
  });
});

describe("ms", () => {
  it("keeps millisecond precision below one second", () => {
    expect(ms(840)).toBe("840 ms");
  });
  it("switches to seconds above one second", () => {
    expect(ms(1500)).toBe("1.50 s");
  });
  it("shows a dash when the value is absent", () => {
    expect(ms(null)).toBe("—");
  });
});

describe("pct", () => {
  it("computes one decimal place", () => {
    expect(pct(1, 3)).toBe("33.3%");
  });
  it("refuses to divide by zero", () => {
    expect(pct(5, 0)).toBe("—");
  });
});

describe("relative", () => {
  const now = new Date("2026-01-01T12:00:00Z");
  it("calls the last minute 'just now'", () => {
    expect(relative("2026-01-01T11:59:30Z", now)).toBe("just now");
  });
  it("counts minutes and days", () => {
    expect(relative("2026-01-01T11:45:00Z", now)).toBe("15m ago");
    expect(relative("2025-12-29T12:00:00Z", now)).toBe("3d ago");
  });
  it("returns a dash for a missing timestamp", () => {
    expect(relative(undefined, now)).toBe("—");
  });
});

describe("stamp", () => {
  it("renders a stable UTC instant", () => {
    expect(stamp("2026-01-01T12:34:56Z")).toBe("2026-01-01 12:34:56Z");
  });
});

describe("keyPrefix", () => {
  it("never renders a full key", () => {
    expect(keyPrefix("atk_abc12345")).toBe("atk_abc12345••••");
  });
});

describe("tone", () => {
  it("maps operational states to signal colours", () => {
    expect(tone("ok")).toBe("ok");
    expect(tone("healthy")).toBe("ok");
    expect(tone("error")).toBe("bad");
    expect(tone("untested")).toBe("idle");
    expect(tone("rejected")).toBe("warn");
    expect(tone("anything-else")).toBe("idle");
  });
});
