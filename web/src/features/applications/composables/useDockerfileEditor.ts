import { computed, ref, toValue, watch, type Ref } from "vue";

import type { Application } from "@/features/applications/api/applications";
import {
  buildArgKeySchema,
  buildArgValueSchema,
  dockerfileContentSchema,
} from "@/features/applications/schemas/applications";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import type { BuildArgRow } from "@/features/applications/utils/buildArgs";

export function useDockerfileEditor(source: Ref<Application | null> | Application | null) {
  const appsStore = useApplicationsStore();

  const content = ref("");
  const args = ref<BuildArgRow[]>([]);
  const saving = ref(false);
  const saveError = ref("");
  const savedContent = ref("");
  const savedArgs = ref<BuildArgRow[]>([]);

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
    () => [toValue(source)?.id, toValue(source)?.updated_at] as const,
    ([id], prev) => {
      if (prev === undefined || id !== prev[0] || !isDirty.value) {
        seed();
      }
    },
    { immediate: true },
  );

  const contentValid = computed<boolean>(
    () => dockerfileContentSchema.safeParse(content.value).success,
  );

  const argsValid = computed<boolean>(() =>
    args.value
      .filter((row) => row.key.trim() !== "" || row.value !== "")
      .every(
        (row) =>
          buildArgKeySchema.safeParse(row.key).success &&
          buildArgValueSchema.safeParse(row.value).success,
      ),
  );

  const saveDisabled = computed<boolean>(
    () =>
      saving.value ||
      toValue(source) === null ||
      !contentValid.value ||
      !argsValid.value,
  );

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
