import { ref, watch } from "vue";

import { getNodeHost } from "@/features/servers/api/host";
import type { NodeHostState } from "@/features/servers/api/host";
import { errorText, isForbidden } from "@/shared/api/errors";

export function useNodeHost(serverId: () => string) {
  const state = ref<NodeHostState | null>(null);
  const loading = ref(false);
  const forbidden = ref(false);
  const loadError = ref("");
  let requestSeq = 0;

  async function load(): Promise<void> {
    const id = serverId();
    if (id === "") {
      return;
    }
    const seq = ++requestSeq;
    loading.value = true;
    loadError.value = "";
    try {
      const next = await getNodeHost(id);
      if (seq !== requestSeq || serverId() !== id) {
        return;
      }
      state.value = next;
      forbidden.value = false;
    } catch (error) {
      if (seq !== requestSeq || serverId() !== id) {
        return;
      }
      forbidden.value = isForbidden(error);
      loadError.value = forbidden.value ? "" : errorText(error);
    } finally {
      if (seq === requestSeq) {
        loading.value = false;
      }
    }
  }

  function apply(origin: string, next: NodeHostState): void {
    if (origin === serverId()) {
      state.value = next;
    }
  }

  watch(serverId, () => {
    state.value = null;
    void load();
  });

  return { state, loading, forbidden, loadError, load, apply };
}
