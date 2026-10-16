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

/**
 * useMfaChallenge drives the second login step (JUS-196): it redeems the
 * pending half-authenticated challenge with an authenticator or recovery code.
 * A 401 is the expected "wrong code" answer, so it renders a curated message
 * instead of the generic server-error summary; a 429 surfaces the shared
 * rate-limit message. The password never survives into this step.
 */
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

  /** focusCode returns the caret to the code field (mount and after an error). */
  function focusCode(): void {
    codeRef.value?.focus();
  }

  /** toggleMode switches between an authenticator and a recovery code. */
  function toggleMode(): void {
    mode.value = mode.value === "totp" ? "recovery" : "totp";
    form.code = "";
    rawError.value = null;
    focusCode();
  }

  /**
   * submit redeems the challenge. "expired" means the token can no longer be
   * redeemed (its lifetime elapsed or the account is locked out), so the caller
   * must send the user back to the credentials step.
   */
  async function submit(): Promise<MfaSubmitOutcome> {
    if (submitting.value) {
      return "failed";
    }
    // A token past its lifetime can only answer 401, so do not strand the user
    // on a dead code step.
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
        // The account is locked out after too many wrong codes; this token
        // cannot succeed again, so start the sign-in over.
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
