import { useDocumentVisibility, useIntervalFn } from "@vueuse/core";
import { ref, watch } from "vue";
import type { Ref } from "vue";

/**
 * useCoarseNow is a coarse clock used to keep relative timestamps ("2 days
 * ago") from going stale on a page that stays open. It ticks once a minute —
 * one timer per caller, never one per rendered row — pauses while the tab is
 * hidden, refreshes on return, and VueUse disposes it with the owning scope.
 */
export function useCoarseNow(intervalMs = 60_000): Ref<number> {
  const now = ref(Date.now());
  const visibility = useDocumentVisibility();
  const { pause, resume } = useIntervalFn(
    () => {
      now.value = Date.now();
    },
    intervalMs,
    { immediate: false },
  );

  watch(
    visibility,
    (state) => {
      if (state === "visible") {
        now.value = Date.now();
        resume();
      } else {
        pause();
      }
    },
    // Sync so a tab that is hidden stops ticking the moment it is hidden (and
    // resumes on the same tick as the visibility change), not on the next
    // scheduler flush.
    { immediate: true, flush: "sync" },
  );

  return now;
}
