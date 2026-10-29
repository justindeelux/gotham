import { computed, ref, toValue, watch, type Ref } from "vue";

import type { Application } from "@/features/applications/api/applications";
import {
  composeContentSchema,
  composeFileSchema,
  composeServiceSchema,
} from "@/features/applications/schemas/applications";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import { extractComposeServiceNames } from "@/shared/utils/compose";

export function useComposeEditor(source: Ref<Application | null> | Application | null) {
  const appsStore = useApplicationsStore();

  const content = ref("");
  const file = ref("");
  const service = ref("");
  const saving = ref(false);
  const saveError = ref("");
  const savedContent = ref("");
  const savedFile = ref("");
  const savedService = ref("");

  function seed(): void {
    const app = toValue(source);
    content.value = app?.compose_content ?? "";
    file.value = app?.compose_file ?? "";
    service.value = app?.compose_service ?? "";
    savedContent.value = content.value;
    savedFile.value = file.value;
    savedService.value = service.value;
    saveError.value = "";
  }

  const isDirty = computed<boolean>(
    () =>
      content.value !== savedContent.value ||
      file.value !== savedFile.value ||
      service.value !== savedService.value,
  );

  watch(
    () => toValue(source)?.updated_at,
    () => {
      if (!isDirty.value) {
        seed();
      }
    },
    { immediate: true },
  );

  const isRepoMode = computed<boolean>(() => savedContent.value.trim() === "");

  const serviceOptions = computed<Array<{ label: string; value: string }>>(() =>
    extractComposeServiceNames(content.value).map((name) => ({
      label: name,
      value: name,
    })),
  );

  const contentValid = computed<boolean>(
    () => composeContentSchema.safeParse(content.value).success,
  );

  const fileValid = computed<boolean>(
    () => composeFileSchema.safeParse(file.value).success,
  );

  const serviceValid = computed<boolean>(
    () => composeServiceSchema.safeParse(service.value).success,
  );

  const saveDisabled = computed<boolean>(() => {
    if (
      saving.value ||
      toValue(source) === null ||
      !isDirty.value ||
      !serviceValid.value
    ) {
      return true;
    }
    return isRepoMode.value ? !fileValid.value : !contentValid.value;
  });

  async function handleSave(): Promise<boolean> {
    const app = toValue(source);
    if (!app || saveDisabled.value) {
      return false;
    }
    saving.value = true;
    saveError.value = "";
    try {
      if (isRepoMode.value) {
        await appsStore.update(app.id, {
          compose_file: file.value.trim(),
          compose_service: service.value.trim(),
        });
      } else {
        await appsStore.update(app.id, {
          compose_content: content.value,
          compose_service: service.value.trim(),
        });
      }
      seed();
      return true;
    } catch (error) {
      saveError.value = error instanceof Error ? error.message : String(error);
      return false;
    } finally {
      saving.value = false;
    }
  }

  return {
    content,
    file,
    service,
    saving,
    saveError,
    isDirty,
    isRepoMode,
    serviceOptions,
    contentValid,
    fileValid,
    serviceValid,
    saveDisabled,
    handleSave,
  };
}
