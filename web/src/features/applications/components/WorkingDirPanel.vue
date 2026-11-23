<script setup lang="ts">
import {
  NButton,
  NCard,
  NFormItem,
  NSelect,
  NSpace,
  NText,
  useMessage,
} from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { Application } from "@/features/applications/api/applications";
import {
  describeApplicationError,
  scanRepository,
  updateApplication,
} from "@/features/applications/api/applications";
import { workingDirSchema } from "@/features/applications/schemas/applications";
import { useApplicationsStore } from "@/features/applications/stores/applications";

interface Props {
  application: Application;
  canWrite: boolean;
}

const props = defineProps<Props>();

const { t } = useI18n();
const message = useMessage();
const appsStore = useApplicationsStore();

const workingDir = ref<string>(props.application.working_dir ?? "");
const folders = ref<string[]>([]);
const scanning = ref<boolean>(false);
const saving = ref<boolean>(false);
const error = ref<string | null>(null);

watch(
  () => props.application.working_dir,
  (next) => {
    workingDir.value = next ?? "";
  },
);

const folderOptions = computed<Array<{ label: string; value: string }>>(() => [
  { label: String(t("applications.workingDir.root")), value: "" },
  ...folders.value.map((folder) => ({ label: folder, value: folder })),
]);

const valid = computed<boolean>(() =>
  workingDirSchema.safeParse(workingDir.value).success,
);

const dirty = computed<boolean>(
  () => workingDir.value.trim() !== (props.application.working_dir ?? ""),
);

// The folder list comes from the same scan the create wizard uses, so the
// picker offers exactly the paths the control plane accepts.
async function loadFolders(): Promise<void> {
  scanning.value = true;
  error.value = null;
  try {
    const scan = await scanRepository({
      source_type: props.application.source_type,
      provider: props.application.provider,
      repo: props.application.repo,
      clone_url: props.application.clone_url,
      branch: props.application.branch,
      github_app_id: props.application.github_app_id || undefined,
    });
    if ((scan.error ?? "") !== "") {
      error.value = t("applications.workingDir.scanFailed");
      return;
    }
    folders.value = scan.directories;
  } catch (submitError) {
    error.value = describeApplicationError(submitError);
  } finally {
    scanning.value = false;
  }
}

async function save(): Promise<void> {
  const next = workingDir.value.trim();
  if (!workingDirSchema.safeParse(next).success) {
    error.value = t("applications.workingDir.invalid");
    return;
  }
  error.value = null;
  saving.value = true;
  try {
    await updateApplication(props.application.id, { working_dir: next });
    await appsStore.fetchApplication(props.application.id);
    message.success(t("applications.workingDir.saved"));
  } catch (submitError) {
    error.value = describeApplicationError(submitError);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <NCard :title="t('applications.workingDir.title')">
    <NSpace vertical :size="12">
      <NText depth="3">
        {{ t("applications.workingDir.hint") }}
      </NText>
      <NFormItem :label="t('applications.workingDir.field')">
        <NSelect
          v-model:value="workingDir"
          :options="folderOptions"
          :status="valid ? undefined : 'error'"
          :disabled="!props.canWrite"
          filterable
          tag
          class="mono"
        />
        <span class="working-dir-hint">
          {{ valid ? t("applications.workingDir.hintDetail") : t("applications.workingDir.invalid") }}
        </span>
      </NFormItem>
      <NText v-if="error" depth="3" class="working-dir-error">
        {{ error }}
      </NText>
      <NSpace v-if="props.canWrite" align="center" :size="8">
        <NButton :loading="scanning" tertiary @click="() => void loadFolders()">
          {{ t("applications.workingDir.loadFolders") }}
        </NButton>
        <NButton
          type="primary"
          :loading="saving"
          :disabled="!dirty || !valid"
          @click="() => void save()"
        >
          {{ t("applications.workingDir.save") }}
        </NButton>
        <NText depth="3" class="working-dir-hint">
          {{ t("applications.workingDir.redeployHint") }}
        </NText>
      </NSpace>
    </NSpace>
  </NCard>
</template>

<style scoped>
.mono {
  width: 100%;
  font-family: var(--font-mono);
}

.working-dir-hint {
  font-size: var(--text-xs);
  color: var(--meta);
}

.working-dir-error {
  font-size: var(--text-sm);
  color: var(--danger);
}
</style>
