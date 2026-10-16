import { nextTick, ref, watch } from "vue";

/**
 * useHostUnlock announces and moves focus when a settings section becomes
 * editable after the in-product host-helper install (JUS-185). It exposes a
 * template ref to place on the first field of the section and a flag the
 * template binds to a polite live region, so a keyboard user hears the unlock
 * and lands on the first field instead of the body.
 */
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
