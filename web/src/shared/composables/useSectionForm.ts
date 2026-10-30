import { useMessage } from "naive-ui";
import { ref } from "vue";

import { errorLog, errorText, fieldErrors } from "@/shared/api/errors";

export function useSectionForm<T>(
  save: () => Promise<T>,
  onSaved: (_next: T) => void,
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
      serverErrors.value = fieldErrors(error);
      if (Object.keys(serverErrors.value).length === 0) {
        // The API's log carries the host helper's own reason; show it with the
        // generic message so a failed change is diagnosable from the form.
        errorMessage.value = [errorText(error), errorLog(error)]
          .filter((part) => part !== "")
          .join("\n");
      }
    } finally {
      submitting.value = false;
    }
  }

  return { submitting, serverErrors, errorMessage, submit };
}
