import type { FormInst, FormRules } from "naive-ui";
import { computed, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  describeAuthError,
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
export function useMfaChallenge() {
  const { t } = useI18n();
  const authStore = useAuthStore();

  const formRef = ref<FormInst | null>(null);
  const submitting = ref(false);
  const rawError = ref<unknown>(null);
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

  const rules: FormRules = twoFactorCodeRules();

  /** submit returns true once the session was started. */
  async function submit(): Promise<boolean> {
    if (submitting.value) {
      return false;
    }
    submitting.value = true;
    rawError.value = null;

    try {
      await formRef.value?.validate();
    } catch {
      submitting.value = false;
      return false;
    }

    try {
      await authStore.completeMfaLogin(form.code);
      form.code = "";
      return true;
    } catch (error) {
      rawError.value = error;
      return false;
    } finally {
      submitting.value = false;
    }
  }

  return { formRef, form, rules, submitting, errorMessage, submit };
}
