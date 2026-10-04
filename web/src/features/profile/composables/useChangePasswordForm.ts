import type { FormInst, FormRules } from "naive-ui";
import { useMessage } from "naive-ui";
import { computed, reactive, ref } from "vue";

import { describeAuthError, strengthOf, useAuthStore } from "@/features/auth";
import { changePassword } from "@/features/profile/api/profile";
import { changePasswordRules } from "@/features/profile/schemas/profile";

// Error convention (shared with the auth pages): client-side validation
// errors render inline on the field via NFormItem; server-side submit
// failures render once in the NAlert above the form.
interface ChangePasswordForm {
  currentPassword: string;
  newPassword: string;
  confirmPassword: string;
}

/**
 * useChangePasswordForm holds the change-password panel state. Created per
 * panel mount and dropped on unmount, so typed passwords never survive a
 * route change. On success the returned token pair is installed through
 * setSession so the sidebar and session keep working, and the local fields
 * are cleared.
 */
export function useChangePasswordForm() {
  const authStore = useAuthStore();
  const message = useMessage();

  // Accounts without a password (OAuth-created) set one without a current
  // password; a missing flag (older control plane) keeps the field, and the
  // server ignores a current password it does not need. Reactive over the
  // store: fetchMe resolves after mount and flips this without a reload.
  const hasPassword = computed<boolean>(
    () => authStore.user?.has_password ?? true,
  );

  const formRef = ref<FormInst | null>(null);
  const submitting = ref(false);
  const errorMessage = ref("");
  const form = reactive<ChangePasswordForm>({
    currentPassword: "",
    newPassword: "",
    confirmPassword: "",
  });

  const strength = computed<number>(() => strengthOf(form.newPassword));

  // The confirm rule reads the live new password through a reader (not a
  // snapshot), so retyping the password revalidates the confirmation. Built
  // reactively from hasPassword so a late fetchMe flips the current field.
  const rules = computed<FormRules>(() =>
    changePasswordRules(hasPassword.value, () => form.newPassword),
  );

  async function handleSubmit(): Promise<void> {
    // Guard: a click on a submit button plus the native submit (or Enter
    // plus the submit event) invoke this twice in the same tick; the second
    // call must not start a second request.
    if (submitting.value) {
      return;
    }
    submitting.value = true;
    errorMessage.value = "";

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
      message.success("Password changed. Other devices were signed out.");
    } catch (error) {
      errorMessage.value = describeAuthError(error);
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
