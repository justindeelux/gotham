import { computed, ref } from "vue";

import { useProxyStore } from "@/features/domains/stores/proxy";

export function useRouters() {
  const proxyStore = useProxyStore();
  const query = ref("");

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

  const failedNodes = computed(() =>
    proxyStore.routerNodes.filter((node) => node.error),
  );

  async function refresh(): Promise<void> {
    await proxyStore.fetchRouters();
  }

  return { query, filteredRouters, failedNodes, refresh };
}

export type RoutersState = ReturnType<typeof useRouters>;
