import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/instance-settings/api/instance", () => ({
  confirmControlPlaneURL: vi.fn(),
  revertControlPlaneURL: vi.fn(),
}));

import {
  confirmControlPlaneURL,
  revertControlPlaneURL,
} from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";
import ControlPlaneConfirmCard from "@/features/instance-settings/components/ControlPlaneConfirmCard.vue";
import en from "@/features/instance-settings/locales/en";
import vietnamese from "@/features/instance-settings/locales/vi";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const now = new Date("2026-01-01T00:00:00Z");
const pending = {
  previous: "https://old.example.com",
  proposed: "https://new.example.com",
  deadline: new Date(now.getTime() + 120_000).toISOString(),
  synced: true,
};

const settledState: InstanceState = {
  general: {
    control_plane_url: { value: "https://new.example.com", source: "db", locked: false },
    instance_name: { value: "gotham", source: "default", locked: false },
  },
  pending_control_plane_url: null,
};

function mountCard() {
  return mount(ControlPlaneConfirmCard, {
    props: { pending },
    global: { plugins: [i18n] },
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(now);
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { "instance-settings": en });
  i18n.global.mergeLocaleMessage("vi", { "instance-settings": vietnamese });
  syncComposerLocale("en");
  vi.mocked(confirmControlPlaneURL).mockReset().mockResolvedValue(settledState);
  vi.mocked(revertControlPlaneURL).mockReset().mockResolvedValue(settledState);
});

afterEach(() => vi.useRealTimers());

describe("control-plane URL confirm card", () => {
  it("shows the new URL and a silent countdown", () => {
    const wrapper = mountCard();
    expect(wrapper.text()).toContain("https://new.example.com");
    expect(wrapper.text()).toContain("02:00");
    const timer = wrapper.get('[role="timer"]');
    expect(timer.attributes("aria-live")).toBe("off");
    expect(wrapper.get('[role="status"]').text()).toContain("02:00");
    wrapper.unmount();
  });

  it("keeps the change through the confirm endpoint", async () => {
    const wrapper = mountCard();
    const keep = wrapper.findAll("button").find((button) => button.text().includes("Keep change"));
    await keep?.trigger("click");
    await flushPromises();
    expect(confirmControlPlaneURL).toHaveBeenCalledTimes(1);
    expect(wrapper.emitted("settled")?.[0]).toEqual([settledState]);
    wrapper.unmount();
  });

  it("reverts immediately through the revert endpoint", async () => {
    const wrapper = mountCard();
    const revert = wrapper.findAll("button").find((button) => button.text().includes("Revert now"));
    await revert?.trigger("click");
    await flushPromises();
    expect(revertControlPlaneURL).toHaveBeenCalledTimes(1);
    expect(wrapper.emitted("settled")?.[0]).toEqual([settledState]);
    wrapper.unmount();
  });

  it("expires at the deadline", async () => {
    const wrapper = mountCard();
    await vi.advanceTimersByTimeAsync(120_000);
    expect(wrapper.emitted("expired")).toHaveLength(1);
    expect(vi.mocked(confirmControlPlaneURL)).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("renders the copy in Vietnamese", async () => {
    syncComposerLocale("vi");
    const wrapper = mountCard();
    await flushPromises();
    expect(wrapper.text()).toContain(vietnamese.pendingConfirm.title);
    expect(wrapper.text()).toContain("02:00");
    wrapper.unmount();
  });
});
