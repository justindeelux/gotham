import type { FormInst, FormRules } from "naive-ui";
import { useMessage } from "naive-ui";
import { computed, onUnmounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { createVisibleValidation, describeAuthError, useAuthStore } from "@/features/auth";
import { patchDisplayName } from "@/features/profile/api/profile";
import { displayNameRules } from "@/features/profile/schemas/profile";
import { onLocaleChange } from "@/shared/i18n";

// Error convention (shared with the auth pages): client-side validation
// errors render inline on the field via NFormItem; server-side submit
// failures keep the raw error and render once in the NAlert above the form
// through a computed, so a language switch refreshes the banner reactively.
interface DisplayNameForm {
  displayName: string;
}

/**
 * useDisplayNameForm holds the display-name panel state. Created per panel
 * mount and dropped on unmount, so a typed name never survives a route
 * change. On success the auth store user is replaced and persisted, so the
 * sidebar follows without a reload.
 */
export function useDisplayNameForm() {
  const { t } = useI18n();
  const authStore = useAuthStore();
  const message = useMessage();

  const formRef = ref<FormInst | null>(null);
  const submitting = ref(false);
  const rawError = ref<unknown>(null);
  const errorMessage = computed<string>(() =>
    rawError.value === null ? "" : describeAuthError(rawError.value),
  );
  const form = reactive<DisplayNameForm>({
    displayName: authStore.user?.display_name ?? "",
  });

  const rules: FormRules = displayNameRules();

  /**
   * visible tracks paths with currently-shown feedback (input/blur/submit).
   * A language switch revalidates exactly those paths: already-visible
   * errors refresh, pristine fields stay clean, and nothing submits or
   * calls an API. The record clears on success so a cleared form stays
   * pristine across later switches.
   */
  const visible = createVisibleValidation();
  const trackedRules: FormRules = visible.trackRules(rules);
  const stopLocaleWatch = onLocaleChange(() => {
    visible.refreshVisible(formRef);
  });

  onUnmounted(() => {
    stopLocaleWatch();
  });

  async function handleSubmit(): Promise<void> {
    // Guard: a click on a submit button plus the native submit (or Enter
    // plus the submit event) invoke this twice in the same tick; the second
    // call must not start a second request.
    if (submitting.value) {
      return;
    }
    submitting.value = true;
    rawError.value = null;

    try {
      await formRef.value?.validate();
    } catch {
      submitting.value = false;
      return;
    }

    try {
      const trimmed = form.displayName.trim();
      const user = await patchDisplayName(trimmed === "" ? null : trimmed);
      authStore.setUser(user);
      visible.reset();
      message.success(t("profile.displayName.updated"));
    } catch (error) {
      rawError.value = error;
    } finally {
      submitting.value = false;
    }
  }

  return { formRef, submitting, errorMessage, form, rules: trackedRules, handleSubmit };
}
