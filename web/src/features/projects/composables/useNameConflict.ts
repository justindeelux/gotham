import { computed, ref } from "vue";
import type { ComputedRef, Ref } from "vue";

import { isNameTakenError } from "@/features/projects/api/projects";
import { activeLocale, i18n } from "@/shared/i18n";

export interface NameConflictState {
  feedback: ComputedRef<string | undefined>;
  status: ComputedRef<"error" | undefined>;
  clear(): void;
  take(_error: unknown): boolean;
}

export function useNameConflict(): NameConflictState {
  const conflict: Ref<unknown> = ref(null);
  const feedback = computed<string | undefined>(() => {
    if (conflict.value === null) {
      return undefined;
    }
    void activeLocale.value;
    return String(i18n.global.t("projects.conflict.nameTaken"));
  });
  const status = computed<"error" | undefined>(() =>
    conflict.value === null ? undefined : "error",
  );

  function clear(): void {
    conflict.value = null;
  }

  function take(error: unknown): boolean {
    if (!isNameTakenError(error)) {
      return false;
    }
    conflict.value = error;
    return true;
  }

  return { feedback, status, clear, take };
}
