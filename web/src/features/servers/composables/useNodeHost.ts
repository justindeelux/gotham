import { ref, watch } from "vue";

import { getNodeHost } from "@/features/servers/api/host";
import type { NodeHostState } from "@/features/servers/api/host";
import { errorText, isForbidden } from "@/shared/api/errors";

export function useNodeHost(serverId: () => string) {
  const state = ref<NodeHostState | null>(null);
  const loading = ref(false);
  const forbidden = ref(false);
  const loadError = ref("");

  async function load(): Promise<void> {
    const id = serverId();
    if (id === "") {
      return;
    }
    loading.value = true;
    loadError.value = "";
    try {
      state.value = await getNodeHost(id);
      forbidden.value = false;
    } catch (error) {
      forbidden.value = isForbidden(error);
      loadError.value = forbidden.value ? "" : errorText(error);
    } finally {
      loading.value = false;
    }
  }

  function apply(next: NodeHostState): void {
    state.value = next;
  }

  watch(serverId, () => {
    state.value = null;
    void load();
  });

  return { state, loading, forbidden, loadError, load, apply };
}
