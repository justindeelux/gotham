import { computed, ref, watch } from "vue";
import type { ComputedRef } from "vue";
import { useRoute, useRouter } from "vue-router";

import { useAuthStore } from "@/features/auth";

/** DETAIL_TABS is the tab order shared with the URL `tab` query. */
const DETAIL_TABS = [
  "overview",
  "containers",
  "files",
  "terminal",
  "metrics",
  "proxy",
  "settings",
] as const;
export type DetailTab = (typeof DETAIL_TABS)[number];

/** isDetailTab narrows one untrusted query value. */
function isDetailTab(value: unknown): value is DetailTab {
  return (
    typeof value === "string" &&
    (DETAIL_TABS as readonly string[]).includes(value)
  );
}

/**
 * Tab selection and URL sync for the server detail page (JUS-246).
 *
 * The page keeps the URL contract (`?tab=terminal`) so the app dock can resume
 * a paused session and a reload lands on the same pane. Operator-only tabs are
 * ignored for a caller who cannot see them; a crafted `?tab=terminal` deep
 * link instead exposes the clear blocked notice.
 */
export function useServerDetailTab(serverId: ComputedRef<string>) {
  const route = useRoute();
  const router = useRouter();
  const authStore = useAuthStore();

  /** tabFromQuery restores the tab after a reload or a dock resume. */
  function tabFromQuery(): DetailTab {
    const value = route.query.tab;
    if (!isDetailTab(value)) {
      return "overview";
    }
    if (
      (value === "files" || value === "terminal") &&
      !authStore.isPlatformAdmin
    ) {
      return "overview";
    }
    return value;
  }

  const activeTab = ref<DetailTab>(tabFromQuery());
  /** blockedTerminal backs the clear notice for a non-operator deep link. */
  const blockedTerminal = computed<boolean>(
    () => route.query.tab === "terminal" && !authStore.isPlatformAdmin,
  );

  let syncingTabQuery = false;
  watch(activeTab, (next) => {
    const desired = next === "terminal" ? "terminal" : null;
    const current = typeof route.query.tab === "string" ? route.query.tab : null;
    if (current === desired) {
      return;
    }
    syncingTabQuery = true;
    const query = { ...route.query };
    if (desired === null) {
      delete query.tab;
    } else {
      query.tab = desired;
    }
    void router
      .replace({
        name: "server-detail",
        params: { id: serverId.value },
        query,
      })
      .finally(() => {
        syncingTabQuery = false;
      });
  });

  watch(
    () => route.query.tab,
    (value) => {
      if (syncingTabQuery) {
        return;
      }
      const next = isDetailTab(value) ? value : "overview";
      const visible =
        (next === "files" || next === "terminal") &&
        !authStore.isPlatformAdmin
          ? "overview"
          : next;
      if (visible !== activeTab.value) {
        activeTab.value = visible;
      }
    },
  );

  watch(serverId, () => {
    activeTab.value = tabFromQuery();
  });

  /** showOverview leaves an operator tab after a demotion or block. */
  function showOverview(): void {
    if (activeTab.value === "terminal" || activeTab.value === "files") {
      activeTab.value = "overview";
    }
  }

  return { activeTab, blockedTerminal, showOverview };
}
