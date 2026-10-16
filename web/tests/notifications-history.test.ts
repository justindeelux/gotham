// JUS-195 delivery history: label helpers and the ChannelCard panel.
//
// The panel is lazy (it reads the history only when opened), so the harness
// proves the click triggers exactly one read and renders the returned rows.
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";

vi.mock("@/features/notifications/api/notifications", async (importOriginal) => {
  const actual =
    await importOriginal<
      typeof import("@/features/notifications/api/notifications")
    >();
  return { ...actual, listDeliveries: vi.fn(), testChannel: vi.fn() };
});

import {
  deliveryEventLabel,
  deliveryStatusLabel,
  listDeliveries,
  testChannel,
} from "@/features/notifications/api/notifications";
import type {
  NotificationChannel,
  NotificationDelivery,
} from "@/features/notifications/api/notifications";
import ChannelCard from "@/features/notifications/components/ChannelCard.vue";
import { provideChannelDialog } from "@/features/notifications/composables/useChannelDialog";
import notificationsEn from "@/features/notifications/locales/en";
import notificationsVi from "@/features/notifications/locales/vi";
import { useTeamsStore } from "@/features/teams";
import {
  i18n,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { notifications: notificationsEn });
  i18n.global.mergeLocaleMessage("vi", { notifications: notificationsVi });
  syncComposerLocale("en");
  vi.mocked(listDeliveries).mockReset();
  vi.mocked(testChannel).mockReset();
});

const channel = {
  id: "ch1",
  team_id: "team-1",
  name: "Deploy alerts",
  kind: "discord",
  enabled: true,
  events: ["deploy_failure"],
  config: {},
  secrets_configured: true,
  created_at: "2026-09-29T11:00:00Z",
  updated_at: "2026-09-29T11:00:00Z",
} as NotificationChannel;

function attempt(overrides: Partial<NotificationDelivery>): NotificationDelivery {
  return {
    id: "d1",
    channel_id: "ch1",
    event_type: "deploy_failure",
    status: "failure",
    error_summary: "connection failed",
    created_at: "2026-09-29T12:00:00Z",
    ...overrides,
  };
}

/** deferred builds a promise a test resolves by hand. */
function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

describe("JUS-195 delivery labels", () => {
  it("localizes status and event labels, keeping wire keys raw", () => {
    expect(deliveryStatusLabel("success")).toBe("Delivered");
    expect(deliveryStatusLabel("failure")).toBe("Failed");
    expect(deliveryEventLabel("deploy_failure")).toBe("Deploy failed");
    expect(deliveryEventLabel("test")).toBe("Test message");
    // An unknown future key renders verbatim, like eventLabel.
    expect(deliveryEventLabel("deploy_exploded")).toBe("deploy_exploded");

    setLocale("vi", null);
    expect(deliveryStatusLabel("success")).toBe("Đã gửi");
    expect(deliveryStatusLabel("failure")).toBe("Thất bại");
    expect(deliveryEventLabel("deploy_failure")).toBe("Triển khai thất bại");
    expect(deliveryEventLabel("test")).toBe("Tin nhắn thử");
  });
});

describe("JUS-195 channel history panel", () => {
  async function cardHarness() {
    setActivePinia(createPinia());
    const teams = useTeamsStore();
    teams.teams = [
      { id: "team-1", name: "Acme", is_personal: false, role: "owner", created_at: "", updated_at: "" },
    ] as never;
    teams.activeTeamId = "team-1";
    const Inner = defineComponent({
      setup() {
        provideChannelDialog();
        return () => h(ChannelCard, { channel });
      },
    });
    const wrapper = mount(
      defineComponent({
        render: () => h(NMessageProvider, null, { default: () => h(Inner) }),
      }),
      { global: { plugins: [i18n] } },
    );
    await flushPromises();
    return wrapper;
  }

  async function openHistory(wrapper: Awaited<ReturnType<typeof cardHarness>>) {
    const entry = wrapper
      .findAll("button")
      .find((button) =>
        ["Delivery history", "Hide delivery history"].includes(button.text()),
      );
    expect(entry, "expected a Delivery history button").toBeDefined();
    await entry!.trigger("click");
    await flushPromises();
    await nextTick();
  }

  it("loads history on open and renders each attempt", async () => {
    vi.mocked(listDeliveries).mockResolvedValueOnce([
      attempt({ id: "d1", status: "failure", error_summary: "connection failed" }),
      attempt({ id: "d2", status: "success", event_type: "test", error_summary: "" }),
    ]);
    const wrapper = await cardHarness();

    // Lazy: nothing is fetched until the panel opens.
    expect(vi.mocked(listDeliveries)).not.toHaveBeenCalled();
    await openHistory(wrapper);

    expect(vi.mocked(listDeliveries)).toHaveBeenCalledTimes(1);
    expect(vi.mocked(listDeliveries)).toHaveBeenCalledWith("team-1", "ch1");
    const rows = wrapper.findAll("[data-test-channel-history-row]");
    expect(rows).toHaveLength(2);
    expect(rows[0].text()).toContain("Failed");
    expect(rows[0].text()).toContain("Deploy failed");
    expect(rows[0].text()).toContain("connection failed");
    expect(rows[1].text()).toContain("Delivered");
    expect(rows[1].text()).toContain("Test message");
    wrapper.unmount();
  });

  it("renders the empty state and a localized read failure", async () => {
    vi.mocked(listDeliveries).mockResolvedValueOnce([]);
    const wrapper = await cardHarness();
    await openHistory(wrapper);
    expect(wrapper.text()).toContain("No delivery attempts yet.");

    vi.mocked(listDeliveries).mockRejectedValueOnce({ message: "", status: 403 });
    await openHistory(wrapper); // closes, then reopens
    await openHistory(wrapper);
    expect(wrapper.find("[data-test-channel-history-error]").text()).toBe(
      "Your team role does not allow this action.",
    );
    wrapper.unmount();
  });

  it("keeps the newest read when two history loads overlap", async () => {
    const first = deferred<NotificationDelivery[]>();
    const second = deferred<NotificationDelivery[]>();
    vi.mocked(listDeliveries)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    vi.mocked(testChannel).mockResolvedValueOnce({
      ok: true,
      message: "delivered",
    });
    const wrapper = await cardHarness();
    const button = (label: string) =>
      wrapper.findAll("button").find((item) => item.text() === label)!;

    // The toggle is disabled while a read is in flight, so the realistic
    // overlap is the send-test refresh racing an open panel's pending read.
    await button("Delivery history").trigger("click"); // read 1 (pending)
    await button("Send test").trigger("click");
    await flushPromises(); // the test resolves and refreshes the open panel
    expect(vi.mocked(listDeliveries)).toHaveBeenCalledTimes(2);

    second.resolve([
      attempt({ id: "new", status: "success", event_type: "test", error_summary: "" }),
    ]);
    await flushPromises();
    expect(wrapper.findAll("[data-test-channel-history-row]")).toHaveLength(1);
    expect(wrapper.text()).toContain("Test message");

    // The stale first read resolves late and must be ignored.
    first.resolve([attempt({ id: "old", error_summary: "stale failure" })]);
    await flushPromises();
    expect(wrapper.findAll("[data-test-channel-history-row]")).toHaveLength(1);
    expect(wrapper.text()).toContain("Test message");
    expect(wrapper.text()).not.toContain("stale failure");
    wrapper.unmount();
  });

  it("announces a send-test result through a polite live region", async () => {
    vi.mocked(testChannel).mockRejectedValueOnce({ message: "", status: 403 });
    const wrapper = await cardHarness();
    const send = wrapper
      .findAll("button")
      .find((button) => button.text() === "Send test");
    expect(send, "expected a Send test button").toBeDefined();
    await send!.trigger("click");
    await flushPromises();

    const result = wrapper.find("[data-test-channel-result]");
    expect(result.exists()).toBe(true);
    expect(result.attributes("role")).toBe("status");
    expect(result.attributes("aria-live")).toBe("polite");
    // No history read is triggered by a send-test while the panel is closed.
    expect(vi.mocked(listDeliveries)).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
