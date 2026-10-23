import { defineStore } from "pinia";
import { computed, ref, watch } from "vue";

import {
  isTaskSnapshotMarker,
  isTerminalStatus,
  parseTaskFrame,
  taskChannel,
} from "@/features/tasks/api/tasks";
import type { TaskEvent } from "@/features/tasks/api/tasks";
import { useTeamsStore } from "@/features/teams";
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
  let subscribedTeam = "";
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

  function subscribeTeam(teamId: string): void {
    if (!socket || teamId === subscribedTeam) {
      return;
    }
    if (subscribedTeam !== "") {
      socket.unsubscribe(taskChannel(subscribedTeam));
    }
    subscribedTeam = teamId;
    snapshotBatch.clear();
    if (teamId !== "") {
      socket.subscribe(taskChannel(teamId));
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
        if (message.channel === taskChannel(subscribedTeam) && isTaskSnapshotMarker(message.raw)) {
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
        }
      },
    });
    subscribeTeam(useTeamsStore().activeTeamId);
    watch(
      () => useTeamsStore().activeTeamId,
      (teamId) => subscribeTeam(teamId),
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
