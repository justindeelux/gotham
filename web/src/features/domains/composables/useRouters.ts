import { computed, ref } from "vue";

import { useProxyStore } from "@/features/domains/stores/proxy";

/**
 * Router list state for the routers tab: a host/service/node search over the
 * generated routers plus the per-node read outcomes. The panel owns the
 * query; the rows come from the shared proxy store, refreshed with the page.
 */
export function useRouters() {
  const proxyStore = useProxyStore();
  const query = ref("");

  /** filteredRouters matches the query against host, service and node. */
  const filteredRouters = computed(() => {
    const needle = query.value.trim().toLowerCase();
    if (needle === "") {
      return proxyStore.routers;
    }
    return proxyStore.routers.filter((router) =>
      [router.host, router.service, router.owner_name ?? "", router.server_name ?? ""]
        .join(" ")
        .toLowerCase()
        .includes(needle),
    );
  });

  /** failedNodes lists the nodes that could not be read (reported, not filled). */
  const failedNodes = computed(() =>
    proxyStore.routerNodes.filter((node) => node.error),
  );

  /** refresh reloads the generated routers. */
  async function refresh(): Promise<void> {
    await proxyStore.fetchRouters();
  }

  return { query, filteredRouters, failedNodes, refresh };
}

export type RoutersState = ReturnType<typeof useRouters>;
