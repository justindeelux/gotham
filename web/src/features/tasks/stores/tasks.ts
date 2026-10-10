import { defineStore } from "pinia";
import { computed, ref, watch } from "vue";

import {
  parseTaskFrame,
  taskChannel,
} from "@/features/tasks/api/tasks";
import type { TaskEvent } from "@/features/tasks/api/tasks";
import { useTeamsStore } from "@/features/teams";
import { getAccessToken } from "@/shared/api/token";
import { useWebSocket } from "@/shared/composables/useWebSocket";

/** How long a succeeded card lingers before auto-dismiss (failed stays). */
export const successDismissMs = 8000;

/** One visible card: the latest event plus its collapsed state. */
export interface TaskCard {
  event: TaskEvent;
  collapsed: boolean;
}

/**
 * Background-task progress (JUS-91).
 *
 * One shared WebSocket subscribes to the active team's task room, so
 * webhook-triggered runs surface without user action. The store lives for
 * the app shell lifetime, so cards survive page navigation; the server
 * replays running tasks after every (re)subscribe, restoring state after a
 * reconnect. Succeeded cards auto-dismiss, failed cards stay until closed.
 */
export const useTasksStore = defineStore("tasks", () => {
  const cards = ref<Record<string, TaskCard>>({});
  const visible = computed<TaskCard[]>(() => Object.values(cards.value));

  let socket: ReturnType<typeof useWebSocket> | null = null;
  let subscribedTeam = "";
  const dismissTimers = new Map<string, ReturnType<typeof setTimeout>>();

  function clearDismissTimer(taskId: string): void {
    const timer = dismissTimers.get(taskId);
    if (timer !== undefined) {
      clearTimeout(timer);
      dismissTimers.delete(taskId);
    }
  }

  /** applyEvent upserts the card for one lifecycle event. */
  function applyEvent(event: TaskEvent): void {
    clearDismissTimer(event.taskId);
    const existing = cards.value[event.taskId];
    cards.value[event.taskId] = {
      event,
      collapsed: existing?.collapsed ?? false,
    };
    if (event.status === "succeeded") {
      const timer = setTimeout(() => {
        dismissTimers.delete(event.taskId);
        delete cards.value[event.taskId];
      }, successDismissMs);
      dismissTimers.set(event.taskId, timer);
    }
  }

  /** dismiss closes one card, cancelling its auto-dismiss timer. */
  function dismiss(taskId: string): void {
    clearDismissTimer(taskId);
    delete cards.value[taskId];
  }

  /** toggleCollapse folds one card to its header row. */
  function toggleCollapse(taskId: string): void {
    const card = cards.value[taskId];
    if (card) {
      card.collapsed = !card.collapsed;
    }
  }

  function subscribeTeam(teamId: string): void {
    if (!socket || teamId === subscribedTeam) {
      return;
    }
    if (subscribedTeam !== "") {
      socket.unsubscribe(taskChannel(subscribedTeam));
    }
    subscribedTeam = teamId;
    if (teamId !== "") {
      socket.subscribe(taskChannel(teamId));
    }
  }

  function ensureSocket(): void {
    if (socket) {
      return;
    }
    // No onReconnectFailed refresh here: the shell socket must stay
    // HTTP-inert (a global refresh POST would pollute unrelated surfaces),
    // and the token getter below re-resolves on every attempt, so rotations
    // from normal app traffic are picked up on the next reconnect.
    socket = useWebSocket({
      url: "/api/v1/ws",
      token: getAccessToken,
      autoConnect: true,
      onMessage: (message) => {
        const event = parseTaskFrame(message.raw);
        if (event) {
          applyEvent(event);
        }
      },
    });
    subscribeTeam(useTeamsStore().activeTeamId);
    watch(
      () => useTeamsStore().activeTeamId,
      (teamId) => subscribeTeam(teamId),
    );
  }

  /** connect opens the shared task socket (idempotent). */
  function connect(): void {
    ensureSocket();
  }

  /** reset clears every card; used by tests and sign-out. */
  function reset(): void {
    for (const taskId of dismissTimers.keys()) {
      clearDismissTimer(taskId);
    }
    cards.value = {};
  }

  return { cards, visible, applyEvent, dismiss, toggleCollapse, connect, reset };
});
