import type { FormInst, FormRules } from "naive-ui";
import { useMessage } from "naive-ui";
import { computed, onUnmounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { createVisibleValidation, describeAuthError, strengthOf, useAuthStore } from "@/features/auth";
import { changePassword } from "@/features/profile/api/profile";
import { changePasswordRules } from "@/features/profile/schemas/profile";
import { onLocaleChange } from "@/shared/i18n";

interface ChangePasswordForm {
  currentPassword: string;
  newPassword: string;
  confirmPassword: string;
}

export function useChangePasswordForm() {
  const { t } = useI18n();
  const authStore = useAuthStore();
  const message = useMessage();

  const hasPassword = computed<boolean>(
    () => authStore.user?.has_password ?? true,
  );

  const formRef = ref<FormInst | null>(null);
  const submitting = ref(false);
  const rawError = ref<unknown>(null);
  const errorMessage = computed<string>(() =>
    rawError.value === null ? "" : describeAuthError(rawError.value),
  );
  const form = reactive<ChangePasswordForm>({
    currentPassword: "",
    newPassword: "",
    confirmPassword: "",
  });

  const strength = computed<number>(() => strengthOf(form.newPassword));

  const visible = createVisibleValidation();
  const rules = computed<FormRules>(() =>
    visible.trackRules(changePasswordRules(hasPassword.value, () => form.newPassword)),
  );

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
      const result = await changePassword({
        ...(hasPassword.value ? { current_password: form.currentPassword } : {}),
        new_password: form.newPassword,
      });
      authStore.setSession(result);
      form.currentPassword = "";
      form.newPassword = "";
      form.confirmPassword = "";
      visible.reset();
      message.success(t("profile.password.changed"));
    } catch (error) {
      rawError.value = error;
    } finally {
      submitting.value = false;
    }
  }

  return {
    formRef,
    submitting,
    errorMessage,
    form,
    rules,
    strength,
    hasPassword,
    handleSubmit,
  };
}
