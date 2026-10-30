import { ref } from "vue";

import { getInstanceSettings } from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";
import { errorText, isForbidden } from "@/shared/api/errors";

export function useInstanceSettings() {
  const state = ref<InstanceState | null>(null);
  const loading = ref(false);
  const forbidden = ref(false);
  const loadError = ref("");

  async function load(): Promise<void> {
    loading.value = true;
    loadError.value = "";
    try {
      state.value = await getInstanceSettings();
      forbidden.value = false;
    } catch (error) {
      forbidden.value = isForbidden(error);
      loadError.value = forbidden.value ? "" : errorText(error);
    } finally {
      loading.value = false;
    }
  }

  function apply(next: InstanceState): void {
    state.value = next;
  }

  return { state, loading, forbidden, loadError, load, apply };
}
