/* global document: readonly, HTMLElement: readonly, HTMLButtonElement: readonly, KeyboardEvent: readonly, setTimeout: readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { computed, nextTick, ref } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/composables/useWebSocket", () => ({
  useWebSocket: () => ({
    status: ref("idle"),
    isOpen: computed(() => false),
    retryCount: ref(0),
    lastError: ref<string | null>(null),
    messages: ref([]),
    connect: () => undefined,
    close: () => undefined,
    send: () => false,
    subscribe: () => undefined,
    unsubscribe: () => undefined,
    clearBuffer: () => undefined,
  }),
}));

vi.mock("@/features/notifications/api/inbox", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/notifications/api/inbox")>();
  return {
    ...actual,
    listNotifications: vi.fn().mockResolvedValue({
      notifications: [],
      unreadCount: 0,
    }),
    markNotificationsSeen: vi.fn(),
  };
});

import NotificationBell from "@/features/notifications/components/NotificationBell.vue";
import { useNotificationInboxStore } from "@/features/notifications/stores/inbox";
import { useAuthStore } from "@/features/auth";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const stub = { template: "<div />" };

async function mountBell() {
  const pinia = createPinia();
  setActivePinia(pinia);
  const auth = useAuthStore();
  auth.user = {
    id: "u-1",
    email: "ada@gotham.dev",
    created_at: "2026-01-01T00:00:00Z",
    role: "member",
  };
  auth.accessToken = "token";
  auth.refreshToken = "refresh";
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "dashboard", component: stub },
      { path: "/login", name: "login", component: stub },
    ],
  });
  await router.push("/");
  await router.isReady();
  const wrapper = mount(NotificationBell, {
    attachTo: document.body,
    global: { plugins: [pinia, router, i18n] },
  });
  await flushPromises();
  return { wrapper, inbox: useNotificationInboxStore() };
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  document.body.innerHTML = "";
  vi.clearAllMocks();
});

describe("notification bell", () => {
  it("shows the unread count capped at 99+ and hides it at zero", async () => {
    const { wrapper, inbox } = await mountBell();
    inbox.unreadCount = 120;
    await nextTick();

    const bell = wrapper.find(".notif-bell");
    expect(bell.attributes("aria-label")).toBe("Notifications, 120 unread");
    expect(wrapper.find(".notif-bell-badge .n-badge-sup").text()).toContain(
      "99+",
    );

    inbox.unreadCount = 0;
    await nextTick();

    expect(wrapper.find(".notif-bell").attributes("aria-label")).toBe(
      "Notifications",
    );
    expect(wrapper.find(".notif-bell-badge .n-badge-sup").exists()).toBe(false);
    wrapper.unmount();
  });

  it("opens an accessible popover with an empty state", async () => {
    const { wrapper } = await mountBell();
    const bell = wrapper.find(".notif-bell");
    (bell.element as HTMLElement).focus();

    await bell.trigger("click");
    await flushPromises();
    await nextTick();

    const dialog = document.querySelector(".notif-inbox");
    expect(dialog).not.toBeNull();
    expect(dialog?.getAttribute("role")).toBe("dialog");
    expect(dialog?.getAttribute("aria-label")).toBe("Notifications");
    expect(dialog?.querySelector(".notif-inbox-empty")?.textContent?.trim()).toBe(
      "No notifications yet.",
    );
    expect(document.activeElement).toBe(bell.element);
    wrapper.unmount();
  });

  it("moves keyboard focus into the popover, traps Tab and restores it on Escape", async () => {
    const { wrapper, inbox } = await mountBell();
    inbox.items = [
      item("n-1", "2026-10-26T11:00:00Z"),
      item("n-2", "2026-10-26T12:00:00Z"),
    ];
    inbox.unreadCount = 2;
    await nextTick();

    const bell = wrapper.find(".notif-bell");
    (bell.element as HTMLElement).focus();
    await bell.trigger("keydown", { key: "Enter" });
    await bell.trigger("click");
    await flushPromises();
    await new Promise((resolve) => setTimeout(resolve, 20));

    const dialog = document.querySelector(".notif-inbox") as HTMLElement | null;
    expect(dialog).not.toBeNull();
    const items = Array.from(
      dialog!.querySelectorAll<HTMLButtonElement>("button.notif-inbox-item"),
    );
    expect(items).toHaveLength(2);
    expect(document.activeElement).toBe(items[0]);

    items[1].dispatchEvent(
      new KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true }),
    );
    expect(document.activeElement).toBe(items[0]);

    items[0].dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Tab",
        shiftKey: true,
        bubbles: true,
        cancelable: true,
      }),
    );
    expect(document.activeElement).toBe(items[1]);

    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }),
    );
    await flushPromises();
    await nextTick();

    expect(bell.attributes("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(bell.element);
    wrapper.unmount();
  });
});

function item(id: string, createdAt: string) {
  return {
    id,
    event_key: "deploy_success",
    resource_type: "application",
    resource_id: `app-${id}`,
    project_id: "proj-1",
    name: `web-${id}`,
    host: "web.example.com",
    error: "",
    created_at: createdAt,
  };
}
