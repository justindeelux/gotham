import { useDocumentVisibility, useIntervalFn } from "@vueuse/core";
import { ref, watch } from "vue";
import type { Ref } from "vue";

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
    { immediate: true, flush: "sync" },
  );

  return now;
}
