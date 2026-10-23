import { useMessage } from "naive-ui";
import { ref } from "vue";

import { errorText, serverFieldErrors } from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";

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
