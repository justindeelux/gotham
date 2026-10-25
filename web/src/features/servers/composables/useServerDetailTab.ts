import { computed, ref, watch } from "vue";
import type { ComputedRef } from "vue";
import { useRoute, useRouter } from "vue-router";

import { useAuthStore } from "@/features/auth";

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

function isDetailTab(value: unknown): value is DetailTab {
  return (
    typeof value === "string" &&
    (DETAIL_TABS as readonly string[]).includes(value)
  );
}

export function useServerDetailTab(serverId: ComputedRef<string>) {
  const route = useRoute();
  const router = useRouter();
  const authStore = useAuthStore();

  function tabFromQuery(): DetailTab {
    const value = route.query.tab;
    if (!isDetailTab(value)) {
      return "overview";
    }
    if (
      (value === "files" || value === "terminal") &&
      !authStore.isOwner
    ) {
      return "overview";
    }
    return value;
  }

  const activeTab = ref<DetailTab>(tabFromQuery());
  const blockedTerminal = computed<boolean>(
    () => route.query.tab === "terminal" && !authStore.isOwner,
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
        !authStore.isOwner
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

  function showOverview(): void {
    if (activeTab.value === "terminal" || activeTab.value === "files") {
      activeTab.value = "overview";
    }
  }

  return { activeTab, blockedTerminal, showOverview };
}
