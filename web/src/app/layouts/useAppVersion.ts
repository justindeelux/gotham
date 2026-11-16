import { computed, onMounted, ref } from "vue";
import type { ComputedRef, Ref } from "vue";

import { formatVersionTag, getInstanceInfo } from "@/features/version";

export function useAppVersion(): {
  versionTag: ComputedRef<string | null>;
  controlPlanePort: Ref<number | null>;
} {
  const runningVersion = ref<string | null>(null);
  const controlPlanePort = ref<number | null>(null);

  const versionTag = computed<string | null>(() => formatVersionTag(runningVersion.value));

  onMounted(async () => {
    try {
      const info = await getInstanceInfo();
      runningVersion.value = info.version;
      controlPlanePort.value = info.port && info.port > 0 ? info.port : null;
    } catch {
      runningVersion.value = null;
      controlPlanePort.value = null;
    }
  });

  return { versionTag, controlPlanePort };
}
