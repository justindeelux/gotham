import { computed, onMounted, ref } from "vue";
import type { ComputedRef } from "vue";

import { formatVersionTag, getVersion } from "@/features/version";

export function useAppVersion(): { versionTag: ComputedRef<string | null> } {
  const runningVersion = ref<string | null>(null);

  const versionTag = computed<string | null>(() => formatVersionTag(runningVersion.value));

  onMounted(async () => {
    try {
      runningVersion.value = await getVersion();
    } catch {
      runningVersion.value = null;
    }
  });

  return { versionTag };
}
