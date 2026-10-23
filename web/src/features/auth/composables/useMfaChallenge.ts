import type { FormInst, FormRules } from "naive-ui";
import { computed, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  describeAuthError,
  isRateLimited,
  isUnauthorized,
  twoFactorCodeRules,
  useAuthStore,
} from "@/features/auth";

export type MfaSubmitOutcome = "success" | "failed" | "expired";

export function useMfaChallenge() {
  const { t } = useI18n();
  const authStore = useAuthStore();

  const formRef = ref<FormInst | null>(null);
  const codeRef = ref<{ focus: () => void } | null>(null);
  const submitting = ref(false);
  const rawError = ref<unknown>(null);
  const mode = ref<"totp" | "recovery">("totp");
  const form = reactive({ code: "" });

  const errorMessage = computed<string>(() => {
    if (rawError.value === null) {
      return "";
    }
    if (isUnauthorized(rawError.value)) {
      return t("auth.mfa.invalidCode");
    }
    return describeAuthError(rawError.value);
  });

  const placeholder = computed<string>(() =>
    mode.value === "totp"
      ? t("auth.mfa.codePlaceholder")
      : t("auth.mfa.recoveryPlaceholder"),
  );

  const rules: FormRules = twoFactorCodeRules();

  function focusCode(): void {
    codeRef.value?.focus();
  }

  function toggleMode(): void {
    mode.value = mode.value === "totp" ? "recovery" : "totp";
    form.code = "";
    rawError.value = null;
    focusCode();
  }

  async function submit(): Promise<MfaSubmitOutcome> {
    if (submitting.value) {
      return "failed";
    }
    if (!authStore.isMfaChallengeLive()) {
      authStore.clearMfaChallenge();
      return "expired";
    }

    submitting.value = true;
    rawError.value = null;

    try {
      await formRef.value?.validate();
    } catch {
      submitting.value = false;
      return "failed";
    }

    try {
      await authStore.completeMfaLogin(form.code);
      form.code = "";
      return "success";
    } catch (error) {
      if (isRateLimited(error)) {
        authStore.clearMfaChallenge();
        return "expired";
      }
      form.code = "";
      rawError.value = error;
      focusCode();
      return "failed";
    } finally {
      submitting.value = false;
    }
  }

  return {
    formRef,
    codeRef,
    form,
    mode,
    placeholder,
    rules,
    submitting,
    errorMessage,
    submit,
    focusCode,
    toggleMode,
  };
}
