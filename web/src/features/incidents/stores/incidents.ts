import { defineStore } from "pinia";
import { ref } from "vue";

import {
  acknowledgeIncident,
  incidentErrorStatus,
  incidentPageLimit,
  listIncidents,
  resolvedIncidentLimit,
  snoozeIncident,
  snoozeUntil,
} from "@/features/incidents/api/incidents";
import type {
  Incident,
  IncidentSeverity,
} from "@/features/incidents/api/incidents";

export const incidentPollMs = 60_000;

export type IncidentLoadState = "idle" | "loading" | "ready" | "error" | "unavailable";

export const useIncidentsStore = defineStore("incidents", () => {
  const items = ref<Incident[]>([]);
  const resolved = ref<Incident[]>([]);
  const activeCount = ref(0);
  const nextCursor = ref("");
  const loading = ref(false);
  const loadingMore = ref(false);
  const resolvedLoading = ref(false);
  const resolvedLoaded = ref(false);
  const error = ref<unknown>(null);
  const resolvedError = ref<unknown>(null);
  const state = ref<IncidentLoadState>("idle");
  const severity = ref<IncidentSeverity | "">("");
  const resourceType = ref("");

  let lastRefreshAt = 0;
  let refreshSeq = 0;
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  function classify(errorValue: unknown): IncidentLoadState {
    const status = incidentErrorStatus(errorValue);
    return status === 404 || status === 503 ? "unavailable" : "error";
  }

  async function refresh(): Promise<void> {
    const token = ++refreshSeq;
    if (items.value.length === 0) {
      loading.value = true;
    }
    error.value = null;
    try {
      const page = await listIncidents({
        status: "active",
        severity: severity.value || undefined,
        resourceType: resourceType.value || undefined,
        limit: incidentPageLimit,
      });
      if (token !== refreshSeq) {
        return;
      }
      items.value = page.incidents;
      activeCount.value = page.active_count;
      nextCursor.value = page.next_cursor ?? "";
      state.value = "ready";
      lastRefreshAt = Date.now();
    } catch (errorValue) {
      if (token !== refreshSeq) {
        return;
      }
      error.value = errorValue;
      state.value = classify(errorValue);
    } finally {
      if (token === refreshSeq) {
        loading.value = false;
      }
    }
  }

  async function loadMore(): Promise<void> {
    if (!nextCursor.value || loadingMore.value) {
      return;
    }
    loadingMore.value = true;
    try {
      const page = await listIncidents({
        status: "active",
        severity: severity.value || undefined,
        resourceType: resourceType.value || undefined,
        limit: incidentPageLimit,
        cursor: nextCursor.value,
      });
      items.value = [...items.value, ...page.incidents];
      activeCount.value = page.active_count;
      nextCursor.value = page.next_cursor ?? "";
    } catch (errorValue) {
      error.value = errorValue;
    } finally {
      loadingMore.value = false;
    }
  }

  async function refreshResolved(): Promise<void> {
    resolvedLoading.value = true;
    resolvedError.value = null;
    try {
      const page = await listIncidents({
        status: "resolved",
        limit: resolvedIncidentLimit,
      });
      resolved.value = page.incidents;
      resolvedLoaded.value = true;
    } catch (errorValue) {
      resolvedError.value = errorValue;
    } finally {
      resolvedLoading.value = false;
    }
  }

  function replaceItem(updated: Incident): void {
    const index = items.value.findIndex((item) => item.id === updated.id);
    if (index >= 0) {
      items.value.splice(index, 1, updated);
    }
  }

  async function acknowledge(id: string): Promise<void> {
    replaceItem(await acknowledgeIncident(id));
  }

  async function snooze(id: string, hours: 1 | 24): Promise<void> {
    replaceItem(await snoozeIncident(id, snoozeUntil(hours)));
  }

  function setSeverity(value: IncidentSeverity | ""): void {
    severity.value = value;
    items.value = [];
    activeCount.value = 0;
    nextCursor.value = "";
    void refresh();
  }

  function setResourceType(value: string): void {
    resourceType.value = value;
    items.value = [];
    activeCount.value = 0;
    nextCursor.value = "";
    void refresh();
  }

  function poll(): void {
    if (typeof document !== "undefined" && document.visibilityState === "hidden") {
      return;
    }
    void refresh();
  }

  function onVisibilityChange(): void {
    if (document.visibilityState !== "visible") {
      return;
    }
    if (Date.now() - lastRefreshAt >= incidentPollMs) {
      void refresh();
    }
  }

  function onOnline(): void {
    void refresh();
  }

  function start(): void {
    void refresh();
    if (pollTimer !== null) {
      return;
    }
    pollTimer = setInterval(poll, incidentPollMs);
    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", onVisibilityChange);
    }
    if (typeof window !== "undefined") {
      window.addEventListener("online", onOnline);
    }
  }

  function stop(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
    if (typeof document !== "undefined") {
      document.removeEventListener("visibilitychange", onVisibilityChange);
    }
    if (typeof window !== "undefined") {
      window.removeEventListener("online", onOnline);
    }
  }

  function reset(): void {
    stop();
    refreshSeq += 1;
    items.value = [];
    resolved.value = [];
    activeCount.value = 0;
    nextCursor.value = "";
    loading.value = false;
    loadingMore.value = false;
    resolvedLoading.value = false;
    resolvedLoaded.value = false;
    error.value = null;
    resolvedError.value = null;
    state.value = "idle";
    severity.value = "";
    resourceType.value = "";
  }

  return {
    items,
    resolved,
    activeCount,
    nextCursor,
    loading,
    loadingMore,
    resolvedLoading,
    resolvedLoaded,
    error,
    resolvedError,
    state,
    severity,
    resourceType,
    refresh,
    loadMore,
    refreshResolved,
    acknowledge,
    snooze,
    setSeverity,
    setResourceType,
    start,
    stop,
    reset,
  };
});
