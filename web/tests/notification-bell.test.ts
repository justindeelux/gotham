/* global document: readonly */
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

    await wrapper.find(".notif-bell").trigger("click");
    await flushPromises();
    await nextTick();

    const dialog = document.querySelector(".notif-inbox");
    expect(dialog).not.toBeNull();
    expect(dialog?.getAttribute("role")).toBe("dialog");
    expect(dialog?.getAttribute("aria-label")).toBe("Notifications");
    expect(dialog?.querySelector(".notif-inbox-empty")?.textContent?.trim()).toBe(
      "No notifications yet.",
    );
    wrapper.unmount();
  });
});
