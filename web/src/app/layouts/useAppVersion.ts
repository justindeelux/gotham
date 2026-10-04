import { computed, onMounted, ref } from "vue";
import type { ComputedRef } from "vue";

import { formatVersionTag, getVersion } from "@/features/version";

/**
 * Sidebar-head version tag. Null while loading or on error: the tag hides
 * rather than showing a stale literal.
 */
export function useAppVersion(): { versionTag: ComputedRef<string | null> } {
  // runningVersion is the control-plane binary version from GET /v1/version.
  const runningVersion = ref<string | null>(null);

  /**
   * versionTag is the sidebar-head tag text (see formatVersionTag): null while
   * loading or on error, so the tag hides rather than showing a stale literal.
   */
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
