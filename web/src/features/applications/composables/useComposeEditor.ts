import { computed, ref, toValue, watch, type Ref } from "vue";

import type { Application } from "@/features/applications/api/applications";
import {
  composeContentSchema,
  composeFileSchema,
  composeServiceSchema,
} from "@/features/applications/schemas/applications";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import { extractComposeServiceNames } from "@/features/applications/utils/compose";

/**
 * Draft state behind the compose editor of one compose application (the
 * detail page). A pasted document saves its text and web service together;
 * a repo-backed one saves the file path and web service. A redeploy always
 * ups the latest stored file. An unrelated update to the row (domain save,
 * rename) bumps updated_at without touching the draft: re-seeding only
 * lands while the draft matches the last seeded snapshot, so in-progress
 * edits survive.
 */
export function useComposeEditor(source: Ref<Application | null> | Application | null) {
  const appsStore = useApplicationsStore();

  const content = ref("");
  const file = ref("");
  const service = ref("");
  const saving = ref(false);
  const saveError = ref("");
  /** saved* snapshot the last seeded row for dirty checks. */
  const savedContent = ref("");
  const savedFile = ref("");
  const savedService = ref("");

  /** seed copies the stored row into the draft and snapshots it clean. */
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

  /** isDirty reports whether the draft differs from the last seeded row. */
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

  /** isRepoMode is true when the stored row references a repository file. */
  const isRepoMode = computed<boolean>(() => savedContent.value.trim() === "");

  /** serviceOptions suggests the web service from the draft text. */
  const serviceOptions = computed<Array<{ label: string; value: string }>>(() =>
    extractComposeServiceNames(content.value).map((name) => ({
      label: name,
      value: name,
    })),
  );

  /** contentValid mirrors the backend creation gate (non-empty, 256 KiB). */
  const contentValid = computed<boolean>(
    () => composeContentSchema.safeParse(content.value).success,
  );

  /** fileValid mirrors the backend gate for the in-repo path. */
  const fileValid = computed<boolean>(
    () => composeFileSchema.safeParse(file.value).success,
  );

  /** serviceValid mirrors the backend gate for the routed service. */
  const serviceValid = computed<boolean>(
    () => composeServiceSchema.safeParse(service.value).success,
  );

  /** saveDisabled gates the save on a valid draft and a loaded row. */
  const saveDisabled = computed<boolean>(() => {
    if (saving.value || toValue(source) === null || !serviceValid.value) {
      return true;
    }
    return isRepoMode.value ? !fileValid.value : !contentValid.value;
  });

  /** handleSave writes the draft; the store merges the row, so the page refreshes. */
  async function handleSave(): Promise<void> {
    const app = toValue(source);
    if (!app || saveDisabled.value) {
      return;
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
    } catch (error) {
      saveError.value = error instanceof Error ? error.message : String(error);
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
