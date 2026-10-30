import { onUnmounted, watch } from "vue";
import type { Ref } from "vue";

import { useServersStore } from "@/features/servers/stores/servers";

/**
 * Keep the shared server poll armed only while the usage panel is on screen.
 * The store owns the single timer; this only adds and removes a subscriber.
 */
export function useServerUsageRefresh(activeTab: Ref<string>) {
  const serversStore = useServersStore();
  let subscribed = false;

  function sync(): void {
    const wanted = activeTab.value === "overview";
    if (wanted && !subscribed) {
      serversStore.pollServers();
      subscribed = true;
      return;
    }
    if (!wanted && subscribed) {
      serversStore.stopPolling();
      subscribed = false;
    }
  }

  watch(activeTab, sync, { immediate: true });

  onUnmounted(() => {
    if (!subscribed) {
      return;
    }
    serversStore.stopPolling();
    subscribed = false;
  });
}
