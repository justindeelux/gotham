import type { FormInst, FormRules } from "naive-ui";
import { useMessage } from "naive-ui";
import { computed, onUnmounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { createVisibleValidation, describeAuthError, useAuthStore } from "@/features/auth";
import { patchDisplayName } from "@/features/profile/api/profile";
import { displayNameRules } from "@/features/profile/schemas/profile";
import { onLocaleChange } from "@/shared/i18n";

interface DisplayNameForm {
  displayName: string;
}

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

  const visible = createVisibleValidation();
  const trackedRules: FormRules = visible.trackRules(rules);
  const stopLocaleWatch = onLocaleChange(() => {
    visible.refreshVisible(formRef);
  });

  onUnmounted(() => {
    stopLocaleWatch();
  });

  async function handleSubmit(): Promise<void> {
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
