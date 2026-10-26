import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/http", () => ({
  http: {
    get: vi.fn(),
    post: vi.fn(),
  },
}));

import { http } from "@/shared/api/http";
import { parseNotificationFrame } from "@/features/notifications/api/inbox";
import type { NotificationItem } from "@/features/notifications/api/inbox";
import notificationsEn from "@/features/notifications/locales/en";
import notificationsVi from "@/features/notifications/locales/vi";
import { useNotificationInboxStore } from "@/features/notifications/stores/inbox";
import { notificationText } from "@/features/notifications/utils/inbox";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const getMock = vi.mocked(http.get);
const postMock = vi.mocked(http.post);

const newer: NotificationItem = {
  id: "n-2",
  event_key: "deploy_success",
  resource_type: "application",
  resource_id: "app-1",
  project_id: "proj-1",
  name: "web",
  host: "web.example.com",
  error: "",
  created_at: "2026-10-26T12:00:00.123456Z",
};

const older: NotificationItem = {
  id: "n-1",
  event_key: "backup_failure",
  resource_type: "database",
  resource_id: "db-1",
  project_id: "proj-1",
  name: "orders",
  host: "db.example.com",
  error: "backup failed",
  created_at: "2026-10-26T11:00:00Z",
};

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  getMock.mockReset();
  postMock.mockReset();
  postMock.mockResolvedValue({} as never);
});

describe("notification inbox store", () => {
  it("dedupes socket items by id instead of timestamp", () => {
    const store = useNotificationInboxStore();
    store.pushFromSocket({ ...newer });
    store.pushFromSocket({ ...newer, created_at: "2026-10-26T12:00:00.999999Z" });

    expect(store.items).toHaveLength(1);
    expect(store.unreadCount).toBe(1);
    expect(store.items[0].id).toBe("n-2");
  });

  it("marks seen with the newest displayed created_at and clears the badge", async () => {
    getMock.mockResolvedValue({
      data: { notifications: [newer, older], unread_count: 2 },
    } as never);
    const store = useNotificationInboxStore();
    await store.refresh();

    await store.markSeen();

    expect(postMock).toHaveBeenCalledWith("/notifications/seen", {
      seen_at: newer.created_at,
    });
    expect(store.unreadCount).toBe(0);
  });

  it("keeps unread rows that arrived while the seen request was in flight", async () => {
    getMock.mockResolvedValue({
      data: { notifications: [older], unread_count: 1 },
    } as never);
    const store = useNotificationInboxStore();
    await store.refresh();

    postMock.mockImplementation(async () => {
      store.pushFromSocket(newer);
      return {} as never;
    });
    await store.markSeen();

    expect(store.unreadCount).toBe(1);
  });

  it("parses a notification socket frame and prepends it", () => {
    const frame = JSON.stringify({
      channel: "notifications:u-1",
      type: "notification",
      data: JSON.stringify(newer),
    });
    const parsed = parseNotificationFrame(frame);

    expect(parsed).toMatchObject({ id: "n-2", event_key: "deploy_success" });

    const store = useNotificationInboxStore();
    store.pushFromSocket(parsed!);
    expect(store.items[0].id).toBe("n-2");
    expect(store.unreadCount).toBe(1);
  });

  it("ignores malformed and foreign socket frames", () => {
    expect(parseNotificationFrame("not-json")).toBeNull();
    expect(
      parseNotificationFrame(
        JSON.stringify({ type: "task", data: JSON.stringify(newer) }),
      ),
    ).toBeNull();
    expect(
      parseNotificationFrame(
        JSON.stringify({ type: "notification", data: "{broken" }),
      ),
    ).toBeNull();
  });

  it("resets items and the badge on sign-out", () => {
    const store = useNotificationInboxStore();
    store.pushFromSocket(newer);
    store.pushFromSocket(older);
    expect(store.items).toHaveLength(2);

    store.reset();

    expect(store.items).toEqual([]);
    expect(store.unreadCount).toBe(0);
    expect(store.failure).toBeNull();
  });

  it("keeps an unknown event key and falls back to the generic line", () => {
    const store = useNotificationInboxStore();
    store.pushFromSocket({ ...newer, id: "n-3", event_key: "service_restart" });

    expect(store.items[0].event_key).toBe("service_restart");
    expect(
      notificationText("service_restart", {
        name: "api",
        host: "api.example.com",
        error: "",
      }),
    ).toBe("Update for api on api.example.com");
  });
});

describe("notification inbox copy", () => {
  it("keeps the English and Vietnamese catalogs in sync", () => {
    expect(checkCatalogParity(notificationsEn, notificationsVi)).toEqual([]);
  });

  it("translates the generic line", () => {
    syncComposerLocale("vi");
    try {
      expect(
        notificationText("service_restart", {
          name: "api",
          host: "api.example.com",
          error: "",
        }),
      ).toBe("Cập nhật cho api trên api.example.com");
      expect(i18n.global.te("notifications.inbox.empty")).toBe(true);
    } finally {
      syncComposerLocale("en");
    }
  });
});
