import { defineStore } from "pinia";
import { ref } from "vue";

import {
  acknowledgeIncident,
  incidentErrorStatus,
  incidentMaxPageSize,
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

  async function refresh(showLoading = false): Promise<void> {
    const token = ++refreshSeq;
    // A poll or refocus refresh runs behind the loaded dashboard, so only the
    // first load (and a user-changed filter) may reveal the loading state.
    if (state.value !== "ready" || showLoading) {
      loading.value = true;
    }
    error.value = null;
    // Keep the pages the operator already loaded so a live update does not
    // collapse the list or drop focus on a row further down.
    const limit = Math.min(
      incidentMaxPageSize,
      Math.max(incidentPageLimit, items.value.length),
    );
    try {
      const page = await listIncidents({
        status: "active",
        severity: severity.value || undefined,
        resourceType: resourceType.value || undefined,
        limit,
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
      const status = incidentErrorStatus(errorValue);
      if (status === 401 || status === 403 || status === 404) {
        // Access went away or the read model is absent: never leave the
        // previous grant's rows beside the error.
        items.value = [];
        resolved.value = [];
        resolvedLoaded.value = false;
        activeCount.value = 0;
        nextCursor.value = "";
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
    const token = refreshSeq;
    loadingMore.value = true;
    try {
      const page = await listIncidents({
        status: "active",
        severity: severity.value || undefined,
        resourceType: resourceType.value || undefined,
        limit: incidentPageLimit,
        cursor: nextCursor.value,
      });
      if (token !== refreshSeq) {
        return;
      }
      items.value = [...items.value, ...page.incidents];
      activeCount.value = page.active_count;
      nextCursor.value = page.next_cursor ?? "";
    } catch (errorValue) {
      if (token === refreshSeq) {
        error.value = errorValue;
      }
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
    if (index < 0) {
      return;
    }
    const existing = items.value[index];
    // Ack/snooze responses carry no node grouping map; keep the row's
    // placement and coverage so a child of an offline node stays grouped.
    items.value.splice(index, 1, {
      ...updated,
      grouped_under: existing.grouped_under,
      coverage: existing.coverage,
      delivery_hold: existing.delivery_hold,
    });
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
    void refresh(true);
  }

  function setResourceType(value: string): void {
    resourceType.value = value;
    items.value = [];
    activeCount.value = 0;
    nextCursor.value = "";
    void refresh(true);
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
