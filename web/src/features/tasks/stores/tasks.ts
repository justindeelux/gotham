import { defineStore } from "pinia";
import { computed, ref, watch } from "vue";

import {
  isTaskSnapshotMarker,
  isTerminalStatus,
  parseTaskFrame,
  taskChannel,
} from "@/features/tasks/api/tasks";
import type { TaskEvent } from "@/features/tasks/api/tasks";
import { useProjectsStore } from "@/features/projects";
import { getAccessToken } from "@/shared/api/token";
import { useWebSocket } from "@/shared/composables/useWebSocket";

export const successDismissMs = 8000;

export interface TaskCard {
  event: TaskEvent;
  collapsed: boolean;
  unknown: boolean;
}

export const useTasksStore = defineStore("tasks", () => {
  const cards = ref<Record<string, TaskCard>>({});
  const visible = computed<TaskCard[]>(() => Object.values(cards.value));

  let socket: ReturnType<typeof useWebSocket> | null = null;
  const subscribedChannels = new Set<string>();
  const dismissTimers = new Map<string, ReturnType<typeof setTimeout>>();
  const appliedSeq = new Map<string, number>();
  const snapshotBatch = new Set<string>();

  function clearDismissTimer(taskId: string): void {
    const timer = dismissTimers.get(taskId);
    if (timer !== undefined) {
      clearTimeout(timer);
      dismissTimers.delete(taskId);
    }
  }

  function applyEvent(event: TaskEvent): void {
    const known = appliedSeq.get(event.taskId) ?? 0;
    if (event.seq > 0 && event.seq < known) {
      return;
    }
    appliedSeq.set(event.taskId, Math.max(known, event.seq));
    snapshotBatch.add(event.taskId);
    clearDismissTimer(event.taskId);
    const existing = cards.value[event.taskId];
    cards.value[event.taskId] = {
      event,
      collapsed: existing?.collapsed ?? false,
      unknown: false,
    };
    if (event.status === "succeeded") {
      const timer = setTimeout(() => {
        dismissTimers.delete(event.taskId);
        appliedSeq.delete(event.taskId);
        delete cards.value[event.taskId];
      }, successDismissMs);
      dismissTimers.set(event.taskId, timer);
    }
  }

  function dismiss(taskId: string): void {
    clearDismissTimer(taskId);
    appliedSeq.delete(taskId);
    delete cards.value[taskId];
  }

  function toggleCollapse(taskId: string): void {
    const card = cards.value[taskId];
    if (card) {
      card.collapsed = !card.collapsed;
    }
  }

  function reconcileSnapshot(): void {
    for (const [taskId, card] of Object.entries(cards.value)) {
      if (!isTerminalStatus(card.event.status) && !snapshotBatch.has(taskId)) {
        card.unknown = true;
      }
    }
    snapshotBatch.clear();
  }

  function trackedSequences(): number {
    return appliedSeq.size;
  }

  function syncSubscriptions(): void {
    if (!socket) {
      return;
    }
    const next = new Set(
      useProjectsStore().projects.map((project) => taskChannel(project.id)),
    );
    for (const channel of subscribedChannels) {
      if (!next.has(channel)) {
        socket.unsubscribe(channel);
        subscribedChannels.delete(channel);
      }
    }
    for (const channel of next) {
      if (!subscribedChannels.has(channel)) {
        socket.subscribe(channel);
        subscribedChannels.add(channel);
      }
    }
  }

  function ensureSocket(): void {
    if (socket) {
      return;
    }
    socket = useWebSocket({
      url: "/api/v1/ws",
      token: getAccessToken,
      autoConnect: true,
      onMessage: (message) => {
        if (message.channel?.startsWith("tasks:") && isTaskSnapshotMarker(message.raw)) {
          reconcileSnapshot();
          return;
        }
        const event = parseTaskFrame(message.raw);
        if (event) {
          applyEvent(event);
        }
      },
      onStatusChange: (status) => {
        if (status === "open") {
          snapshotBatch.clear();
          syncSubscriptions();
        }
      },
    });
    if (!useProjectsStore().loaded) {
      void useProjectsStore().fetchProjects().catch(() => undefined);
    }
    watch(
      () => useProjectsStore().projects.map((project) => project.id).join(","),
      () => syncSubscriptions(),
    );
  }

  function connect(): void {
    ensureSocket();
  }

  function reset(): void {
    for (const taskId of dismissTimers.keys()) {
      clearDismissTimer(taskId);
    }
    appliedSeq.clear();
    snapshotBatch.clear();
    cards.value = {};
  }

  return { cards, visible, applyEvent, dismiss, toggleCollapse, connect, reconcileSnapshot, reset, trackedSequences };
});
