import { computed, ref } from "vue";
import type { ComputedRef, Ref } from "vue";

import { isNameTakenError } from "@/features/projects/api/projects";
import { activeLocale, i18n } from "@/shared/i18n";

/**
 * Server-side name-taken state for one dialog form (PE-4 fix round 1).
 * A 409 `... name already exists` renders inline on the name field through
 * the NFormItem `feedback`/`validation-status` props; every other failure
 * stays in the dialog alert. Created per mount and dropped on unmount.
 *
 * The raw refusal is retained (classification via `isNameTakenError` never
 * sees translated text); the inline feedback resolves the curated message
 * in the active locale so it refreshes on a language switch.
 */
export interface NameConflictState {
  feedback: ComputedRef<string | undefined>;
  status: ComputedRef<"error" | undefined>;
  clear(): void;
  /**
   * take stores a name-taken refusal for inline display. Returns true when
   * the error was a conflict (the caller then skips the dialog alert).
   */
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

  /** clear drops the conflict, e.g. when the field value changes. */
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
