// The Modified column's relative age is driven by useCoarseNow (JUS-134): one
// coarse timer per caller, paused while the tab is hidden, disposed with the
// scope. These checks pin the tick, the pause/resume and the cleanup.
/* global document:readonly, Event:readonly */
import { effectScope } from "vue";
import type { Ref } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useCoarseNow } from "@/features/files/composables/useCoarseNow";

/** withScope runs `fn` inside a throwaway effect scope. */
function withScope<T>(fn: () => T): { value: T; stop: () => void } {
  const scope = effectScope();
  const value = scope.run(fn) as T;
  return { value, stop: () => scope.stop() };
}

/** setVisibility forces document.visibilityState and fires the change. */
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
    // Disposed: the timer no longer updates the clock.
    expect(now.value).toBe(Date.parse("2026-10-14T12:01:00Z"));
  });

  it("pauses while the tab is hidden and refreshes on return", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-14T12:00:00Z"));
    const { value: now, stop } = withScope<Ref<number>>(() => useCoarseNow(60_000));

    setVisibility("hidden");
    // The tab is hidden for five minutes; the paused clock does not tick.
    vi.advanceTimersByTime(300_000);
    expect(now.value).toBe(Date.parse("2026-10-14T12:00:00Z"));

    setVisibility("visible");
    // Back on screen: refreshed immediately, not one interval later.
    expect(now.value).toBe(Date.parse("2026-10-14T12:05:00Z"));
    stop();
  });
});
