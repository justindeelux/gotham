import { useMessage } from "naive-ui";
import { ref } from "vue";

import { errorText, serverFieldErrors } from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";

/**
 * useSectionForm wraps one section's save call: it tracks the submitting flag,
 * maps the server's per-field 400 messages onto form paths and reports other
 * failures as a banner. Server messages are shown as received (English).
 */
export function useSectionForm(
  save: () => Promise<InstanceState>,
  onSaved: (_next: InstanceState) => void,
  successText: () => string,
) {
  const message = useMessage();
  const submitting = ref(false);
  const serverErrors = ref<Record<string, string>>({});
  const errorMessage = ref("");

  async function submit(): Promise<void> {
    if (submitting.value) {
      return;
    }
    submitting.value = true;
    serverErrors.value = {};
    errorMessage.value = "";
    try {
      onSaved(await save());
      message.success(successText());
    } catch (error) {
      serverErrors.value = serverFieldErrors(error);
      if (Object.keys(serverErrors.value).length === 0) {
        errorMessage.value = errorText(error);
      }
    } finally {
      submitting.value = false;
    }
  }

  return { submitting, serverErrors, errorMessage, submit };
}
