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

/** How long a succeeded card lingers before auto-dismiss (failed stays). */
export const successDismissMs = 8000;

/** One visible card: the latest event plus its collapsed state. */
export interface TaskCard {
  event: TaskEvent;
  collapsed: boolean;
  /**
   * True when the task vanished from a snapshot replay: it finished while
   * the socket was down, so the outcome is unknown. The card stays (with
   * its log link) instead of lying about success or failure.
   */
  unknown: boolean;
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
  /** Highest sequence applied per task; older frames are stale replays. */
  const appliedSeq = new Map<string, number>();
  /**
   * Task IDs seen since the previous snapshot marker: the replay batch the
   * next marker closes. Cleared on every marker and team switch.
   */
  const snapshotBatch = new Set<string>();

  function clearDismissTimer(taskId: string): void {
    const timer = dismissTimers.get(taskId);
    if (timer !== undefined) {
      clearTimeout(timer);
      dismissTimers.delete(taskId);
    }
  }

  /** applyEvent upserts the card for one lifecycle event, ignoring frames
   * older than what the card already shows (a live event racing the
   * subscribe replay must never be overwritten by its own snapshot). */
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

  /** dismiss closes one card, cancelling its auto-dismiss timer. */
  function dismiss(taskId: string): void {
    clearDismissTimer(taskId);
    appliedSeq.delete(taskId);
    delete cards.value[taskId];
  }

  /** toggleCollapse folds one card to its header row. */
  function toggleCollapse(taskId: string): void {
    const card = cards.value[taskId];
    if (card) {
      card.collapsed = !card.collapsed;
    }
  }

  /**
   * reconcileSnapshot closes one snapshot batch: every non-terminal card
   * absent from the batch finished while the socket was down, so it is
   * marked unknown instead of stuck on running forever. Terminal cards and
   * freshly seen tasks are untouched.
   */
  function reconcileSnapshot(): void {
    for (const [taskId, card] of Object.entries(cards.value)) {
      if (!isTerminalStatus(card.event.status) && !snapshotBatch.has(taskId)) {
        card.unknown = true;
      }
    }
    snapshotBatch.clear();
  }

  /** trackedSequences reports the sequence-table size (bounded by cards). */
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
    // A new subscription brings a fresh snapshot batch.
    snapshotBatch.clear();
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
        // The snapshot end marker closes the replay batch: reconcile first,
        // but only for our own team room.
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
        // A fresh connection always triggers a server replay on
        // resubscribe, so pre-disconnect frames must not linger in the
        // batch the next marker closes.
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

  /** connect opens the shared task socket (idempotent). */
  function connect(): void {
    ensureSocket();
  }

  /** reset clears every card; used by tests and sign-out. */
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
