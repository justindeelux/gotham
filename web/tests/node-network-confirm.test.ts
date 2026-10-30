import { defineComponent, h, nextTick } from "vue";
import { mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import NetworkConfirmCard from "@/features/servers/components/host/NetworkConfirmCard.vue";
import type { PendingNetwork } from "@/features/servers/api/host";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

function pendingIn(seconds: number): PendingNetwork {
  const network = {
    dns_servers: ["1.1.1.1"],
    ipv4: { mode: "dhcp", address: "", gateway: "" },
    ipv6: { enabled: false, mode: "dhcp", address: "", gateway: "" },
  };
  return {
    previous: network,
    proposed: network,
    deadline: new Date(Date.now() + seconds * 1000).toISOString(),
  };
}

function mountCard(seconds: number) {
  return mount(
    defineComponent({
      render: () =>
        h(NetworkConfirmCard as never, {
          serverId: "srv-1",
          pending: pendingIn(seconds),
        }),
    }),
    { global: { plugins: [i18n] } },
  );
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-10-30T12:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
});

describe("NetworkConfirmCard countdown", () => {
  it("ticks in a silent timer and announces only at start, 30s and 10s", async () => {
    const wrapper = mountCard(120);
    await nextTick();

    const timer = wrapper.find('[role="timer"]');
    expect(timer.attributes("aria-live")).toBe("off");
    expect(timer.text()).toBe("02:00");

    const status = wrapper.find('[role="status"]');
    expect(status.text()).toContain("will revert in 02:00");

    await vi.advanceTimersByTimeAsync(90_000);
    await nextTick();
    expect(timer.text()).toBe("00:30");
    expect(status.text()).toContain("Less than 30 seconds");

    const atThirty = status.text();
    await vi.advanceTimersByTimeAsync(1_000);
    await nextTick();
    expect(timer.text()).toBe("00:29");
    expect(status.text()).toBe(atThirty);

    await vi.advanceTimersByTimeAsync(19_000);
    await nextTick();
    expect(timer.text()).toBe("00:10");
    expect(status.text()).toContain("Less than 10 seconds");
  });

  it("announces the nearest warning when it appears late", async () => {
    const wrapper = mountCard(20);
    await nextTick();

    expect(wrapper.find('[role="status"]').text()).toContain("Less than 30 seconds");
    expect(wrapper.find('[role="timer"]').text()).toBe("00:20");
  });
});
