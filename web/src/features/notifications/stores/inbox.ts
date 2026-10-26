import { defineStore } from "pinia";
import { ref } from "vue";

import {
  listNotifications,
  markNotificationsSeen,
  notificationsChannel,
  parseNotificationFrame,
} from "@/features/notifications/api/inbox";
import type {
  NotificationItem,
  NotificationPage,
} from "@/features/notifications/api/inbox";
import { getAccessToken } from "@/shared/api/token";
import { useWebSocket } from "@/shared/composables/useWebSocket";

export const inboxPollMs = 60_000;

export const useNotificationInboxStore = defineStore("notification-inbox", () => {
  const items = ref<NotificationItem[]>([]);
  const unreadCount = ref(0);
  const loading = ref(false);
  const failure = ref<unknown>(null);

  let socket: ReturnType<typeof useWebSocket> | null = null;
  let subscribedChannel = "";
  let channel = "";
  let connectedOnce = false;
  let refreshSeq = 0;
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  function applyPage(page: NotificationPage): void {
    items.value = page.notifications;
    unreadCount.value = page.unreadCount;
  }

  async function refresh(): Promise<void> {
    const token = ++refreshSeq;
    const isCurrent = (): boolean => token === refreshSeq;
    loading.value = true;
    failure.value = null;
    try {
      const page = await listNotifications();
      if (!isCurrent()) {
        return;
      }
      applyPage(page);
    } catch (error) {
      if (!isCurrent()) {
        return;
      }
      failure.value = error;
      throw error;
    } finally {
      if (isCurrent()) {
        loading.value = false;
      }
    }
  }

  function pushFromSocket(item: NotificationItem): void {
    if (items.value.some((existing) => existing.id === item.id)) {
      return;
    }
    items.value = [item, ...items.value];
    unreadCount.value += 1;
  }

  async function markSeen(): Promise<void> {
    const newest = items.value[0];
    if (!newest) {
      return;
    }
    const unreadBefore = unreadCount.value;
    await markNotificationsSeen(newest.created_at);
    unreadCount.value = Math.max(0, unreadCount.value - unreadBefore);
  }

  function startPolling(): void {
    if (pollTimer !== null) {
      return;
    }
    pollTimer = setInterval(() => {
      if (typeof document !== "undefined" && document.visibilityState === "hidden") {
        return;
      }
      if (socket?.isOpen.value === true) {
        return;
      }
      void refresh().catch(() => undefined);
    }, inboxPollMs);
  }

  function stopPolling(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  function subscribe(next: string): void {
    if (!socket || next === subscribedChannel) {
      return;
    }
    if (subscribedChannel !== "") {
      socket.unsubscribe(subscribedChannel);
    }
    subscribedChannel = next;
    channel = next;
    socket.subscribe(next);
  }

  function ensureSocket(userId: string): void {
    if (socket) {
      subscribe(notificationsChannel(userId));
      return;
    }
    socket = useWebSocket({
      url: "/api/v1/ws",
      token: getAccessToken,
      autoConnect: true,
      onMessage: (message) => {
        if (channel === "" || message.channel !== channel) {
          return;
        }
        const item = parseNotificationFrame(message.raw);
        if (item) {
          pushFromSocket(item);
        }
      },
      onStatusChange: (status) => {
        if (status !== "open") {
          return;
        }
        subscribe(notificationsChannel(userId));
        if (connectedOnce) {
          void refresh().catch(() => undefined);
          return;
        }
        connectedOnce = true;
      },
    });
    subscribe(notificationsChannel(userId));
  }

  function connect(userId: string): void {
    if (userId === "") {
      return;
    }
    ensureSocket(userId);
    startPolling();
    void refresh().catch(() => undefined);
  }

  function disconnect(): void {
    stopPolling();
    if (socket) {
      if (subscribedChannel !== "") {
        socket.unsubscribe(subscribedChannel);
      }
      socket.close();
      socket = null;
    }
    subscribedChannel = "";
    channel = "";
    connectedOnce = false;
  }

  function reset(): void {
    disconnect();
    refreshSeq += 1;
    items.value = [];
    unreadCount.value = 0;
    loading.value = false;
    failure.value = null;
  }

  return {
    items,
    unreadCount,
    loading,
    failure,
    refresh,
    markSeen,
    pushFromSocket,
    connect,
    disconnect,
    reset,
  };
});
