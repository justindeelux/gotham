/* global document:readonly, Event:readonly */
import { effectScope } from "vue";
import type { Ref } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useCoarseNow } from "@/features/files/composables/useCoarseNow";

function withScope<T>(fn: () => T): { value: T; stop: () => void } {
  const scope = effectScope();
  const value = scope.run(fn) as T;
  return { value, stop: () => scope.stop() };
}

function setVisibility(state: "visible" | "hidden"): void {
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => state,
  });
  document.dispatchEvent(new Event("visibilitychange"));
}

describe("useCoarseNow", () => {
  afterEach(() => {
    vi.useRealTimers();
    setVisibility("visible");
  });

  it("ticks once per interval and stops when the scope is disposed", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-14T12:00:00Z"));
    const { value: now, stop } = withScope<Ref<number>>(() => useCoarseNow(60_000));

    expect(now.value).toBe(Date.parse("2026-10-14T12:00:00Z"));
    vi.advanceTimersByTime(60_000);
    expect(now.value).toBe(Date.parse("2026-10-14T12:01:00Z"));

    stop();
    vi.advanceTimersByTime(60_000);
    expect(now.value).toBe(Date.parse("2026-10-14T12:01:00Z"));
  });

  it("pauses while the tab is hidden and refreshes on return", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-14T12:00:00Z"));
    const { value: now, stop } = withScope<Ref<number>>(() => useCoarseNow(60_000));

    setVisibility("hidden");
    vi.advanceTimersByTime(300_000);
    expect(now.value).toBe(Date.parse("2026-10-14T12:00:00Z"));

    setVisibility("visible");
    expect(now.value).toBe(Date.parse("2026-10-14T12:05:00Z"));
    stop();
  });
});
