import { nextTick, ref, watch } from "vue";

export function useHostUnlock(isEditable: () => boolean) {
  const focusTarget = ref<{ focus: () => void } | null>(null);
  const unlocked = ref(false);
  watch(isEditable, (now, before) => {
    if (now && before === false) {
      unlocked.value = true;
      void nextTick(() => focusTarget.value?.focus?.());
    }
  });
  return { focusTarget, unlocked };
}
