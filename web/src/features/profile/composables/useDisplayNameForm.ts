import type { FormInst, FormRules } from "naive-ui";
import { useMessage } from "naive-ui";
import { reactive, ref } from "vue";

import { describeAuthError, useAuthStore } from "@/features/auth";
import { patchDisplayName } from "@/features/profile/api/profile";
import { displayNameRules } from "@/features/profile/schemas/profile";

// Error convention (shared with the auth pages): client-side validation
// errors render inline on the field via NFormItem; server-side submit
// failures render once in the NAlert above the form.
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
  const authStore = useAuthStore();
  const message = useMessage();

  const formRef = ref<FormInst | null>(null);
  const submitting = ref(false);
  const errorMessage = ref("");
  const form = reactive<DisplayNameForm>({
    displayName: authStore.user?.display_name ?? "",
  });

  const rules: FormRules = displayNameRules();

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
      const trimmed = form.displayName.trim();
      const user = await patchDisplayName(trimmed === "" ? null : trimmed);
      authStore.setUser(user);
      message.success("Display name updated.");
    } catch (error) {
      errorMessage.value = describeAuthError(error);
    } finally {
      submitting.value = false;
    }
  }

  return { formRef, submitting, errorMessage, form, rules, handleSubmit };
}
