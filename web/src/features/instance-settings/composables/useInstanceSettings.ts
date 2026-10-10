import { ref } from "vue";

import {
  errorText,
  getInstanceSettings,
  isForbidden,
} from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";

/** useInstanceSettings loads and holds the instance-settings state of the page. */
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
