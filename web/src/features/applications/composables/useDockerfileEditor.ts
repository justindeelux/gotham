import { computed, ref, toValue, watch, type Ref } from "vue";

import type { Application } from "@/features/applications/api/applications";
import {
  buildArgKeySchema,
  buildArgValueSchema,
  dockerfileContentSchema,
} from "@/features/applications/schemas/applications";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import type { BuildArgRow } from "@/features/applications/utils/buildArgs";

/**
 * Draft state behind the Dockerfile editor of one dockerfile application
 * (the detail page). The stored text and the --build-arg collection save
 * together through the applications API; a redeploy always builds the
 * latest stored text. An unrelated update to the row (domain save, rename)
 * bumps updated_at without touching the draft: re-seeding only lands while
 * the draft matches the last seeded snapshot, so in-progress edits survive.
 */
export function useDockerfileEditor(source: Ref<Application | null> | Application | null) {
  const appsStore = useApplicationsStore();

  const content = ref("");
  const args = ref<BuildArgRow[]>([]);
  const saving = ref(false);
  const saveError = ref("");
  /** savedContent/savedArgs snapshot the last seeded row for dirty checks. */
  const savedContent = ref("");
  const savedArgs = ref<BuildArgRow[]>([]);

  /** seed copies the stored row into the draft and snapshots it clean. */
  function seed(): void {
    const app = toValue(source);
    content.value = app?.dockerfile_content ?? "";
    args.value = Object.entries(app?.build_args ?? {}).map(([key, value]) => ({
      key,
      value,
    }));
    savedContent.value = content.value;
    savedArgs.value = args.value.map((row) => ({ ...row }));
    saveError.value = "";
  }

  /** isDirty reports whether the draft differs from the last seeded row. */
  const isDirty = computed<boolean>(() => {
    if (content.value !== savedContent.value) {
      return true;
    }
    if (args.value.length !== savedArgs.value.length) {
      return true;
    }
    return args.value.some(
      (row, index) =>
        row.key !== savedArgs.value[index]?.key ||
        row.value !== savedArgs.value[index]?.value,
    );
  });

  watch(
    () => toValue(source)?.updated_at,
    () => {
      if (!isDirty.value) {
        seed();
      }
    },
    { immediate: true },
  );

  /** contentValid mirrors the backend creation gate (FROM required, 64 KiB cap). */
  const contentValid = computed<boolean>(
    () => dockerfileContentSchema.safeParse(content.value).success,
  );

  /** argsValid mirrors the backend gate: named rows need a valid key and value. */
  const argsValid = computed<boolean>(() =>
    args.value
      .filter((row) => row.key.trim() !== "" || row.value !== "")
      .every(
        (row) =>
          buildArgKeySchema.safeParse(row.key).success &&
          buildArgValueSchema.safeParse(row.value).success,
      ),
  );

  /** saveDisabled gates the save on a valid draft and a loaded row. */
  const saveDisabled = computed<boolean>(
    () =>
      saving.value ||
      toValue(source) === null ||
      !contentValid.value ||
      !argsValid.value,
  );

  /** handleSave writes the draft; the store merges the row, so the page refreshes. */
  async function handleSave(): Promise<void> {
    const app = toValue(source);
    if (!app || saveDisabled.value) {
      return;
    }
    saving.value = true;
    saveError.value = "";
    try {
      const buildArgs: Record<string, string> = {};
      for (const row of args.value) {
        const key = row.key.trim();
        if (key !== "") {
          buildArgs[key] = row.value;
        }
      }
      await appsStore.update(app.id, {
        dockerfile_content: content.value,
        build_args: buildArgs,
      });
      seed();
    } catch (error) {
      saveError.value = error instanceof Error ? error.message : String(error);
    } finally {
      saving.value = false;
    }
  }

  return {
    content,
    args,
    saving,
    saveError,
    isDirty,
    contentValid,
    argsValid,
    saveDisabled,
    handleSave,
  };
}
