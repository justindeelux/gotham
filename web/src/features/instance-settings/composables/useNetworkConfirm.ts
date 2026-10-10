import { computed, onUnmounted, ref, watch } from "vue";
import type { Ref } from "vue";

/**
 * useNetworkConfirm turns a pending-change deadline into a whole-second
 * countdown and fires onExpire once when it reaches zero (the host has
 * reverted by then, so the page reloads the state).
 */
export function useNetworkConfirm(deadline: Ref<string | null>, onExpire: () => void) {
  const now = ref(Date.now());
  let timer: ReturnType<typeof setInterval> | null = null;

  function stop(): void {
    if (timer !== null) {
      clearInterval(timer);
      timer = null;
    }
  }

  const secondsLeft = computed<number>(() => {
    if (deadline.value === null) {
      return 0;
    }
    return Math.max(0, Math.ceil((Date.parse(deadline.value) - now.value) / 1000));
  });

  const label = computed<string>(() => {
    const total = secondsLeft.value;
    const mm = String(Math.floor(total / 60)).padStart(2, "0");
    const ss = String(total % 60).padStart(2, "0");
    return `${mm}:${ss}`;
  });

  watch(
    deadline,
    (value) => {
      stop();
      if (value === null) {
        return;
      }
      now.value = Date.now();
      timer = setInterval(() => {
        now.value = Date.now();
        if (secondsLeft.value <= 0) {
          stop();
          onExpire();
        }
      }, 1000);
    },
    { immediate: true },
  );

  onUnmounted(stop);
  return { secondsLeft, label };
}
