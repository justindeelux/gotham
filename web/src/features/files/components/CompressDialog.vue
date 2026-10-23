<script setup lang="ts">
import { Archive } from "@lucide/vue";
import { NButton, NFormItem, NInput, NModal, NProgress, NSelect, useMessage } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { describeFilesError } from "@/features/files/api/files";
import type { ArchiveFormat } from "@/features/files/api/files";
import type { FilesEntry } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import {
  archiveExtension,
  defaultArchiveName,
  normalizeEntryName,
  validateEntryName,
} from "@/features/files/utils/files";
import { useNameConflict } from "@/features/projects/composables/useNameConflict";

interface Props {
  show: boolean;
  entries: FilesEntry[];
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
}>();

const { t } = useI18n();
const message = useMessage();
const files = useFilesStore();
const conflict = useNameConflict();

const format = ref<ArchiveFormat>("zip");
const name = ref("");
const touched = ref(false);

const formatOptions: Array<{ label: string; value: ArchiveFormat }> = [
  { label: "zip", value: "zip" },
  { label: "tar", value: "tar" },
  { label: "tar.gz", value: "tar.gz" },
];

watch(
  () => props.show,
  (open) => {
    if (open) {
      format.value = "zip";
      name.value = defaultArchiveName(props.entries, "zip");
      touched.value = false;
      conflict.clear();
    }
  },
);

watch(format, (next, previous) => {
  const trimmed = name.value.trim();
  const previousExt = archiveExtension(previous);
  if (trimmed.toLowerCase().endsWith(previousExt)) {
    name.value =
      trimmed.slice(0, trimmed.length - previousExt.length) + archiveExtension(next);
  }
  conflict.clear();
});

const nameTaken = computed<boolean>(() =>
  files.entries.some((entry) => entry.name === normalizeEntryName(name.value)),
);

const errorKey = computed<string | null>(() =>
  touched.value ? validateEntryName(name.value) : null,
);
const nameError = computed<string>(() => {
  if (errorKey.value) {
    return String(t(errorKey.value));
  }
  if (touched.value && nameTaken.value) {
    return String(t("files.dialogs.nameExists"));
  }
  return "";
});
const validationStatus = computed<"error" | undefined>(() =>
  errorKey.value || (touched.value && nameTaken.value) ? "error" : conflict.status.value,
);

const valid = computed<boolean>(
  () => validateEntryName(name.value) === null && !nameTaken.value,
);

async function submit(): Promise<void> {
  if (files.acting) {
    return;
  }
  touched.value = true;
  if (!valid.value) {
    return;
  }
  try {
    const created = await files.compress(props.entries, normalizeEntryName(name.value), format.value);
    message.success(
      String(t("files.dialogs.compressed", { name: created.name })),
    );
    emit("update:show", false);
  } catch (err) {
    if (conflict.take(err)) {
      return;
    }
    message.error(describeFilesError(err));
  }
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="props.show"
    preset="card"
    style="width: 460px; max-width: 94vw"
    :title="t('files.dialogs.compressTitle')"
    :mask-closable="!files.acting"
    :close-on-esc="!files.acting"
    :closable="!files.acting"
    @update:show="(value: boolean) => !files.acting && emit('update:show', value)"
  >
    <NFormItem :label="t('files.dialogs.compressFormat')">
      <NSelect v-model:value="format" :options="formatOptions" />
    </NFormItem>
    <NFormItem
      :label="t('files.dialogs.compressLabel')"
      :validation-status="validationStatus"
      :feedback="nameError || conflict.feedback.value"
    >
      <NInput
        v-model:value="name"
        class="mono"
        autofocus
        :placeholder="t('files.dialogs.compressPlaceholder')"
        @keydown.enter="() => void submit()"
      />
    </NFormItem>
    <p class="hint small muted">{{ t("files.dialogs.compressHint") }}</p>

    <div v-if="files.acting" class="archive-progress">
      <NProgress
        type="line"
        :percentage="100"
        :processing="true"
        :show-indicator="false"
        :height="6"
      />
      <span class="small muted" role="status" aria-live="polite">{{ t("files.dialogs.compressProgress") }}</span>
    </div>

    <template #footer>
      <div class="dialog-foot">
        <span class="grow small muted">
          {{ t("files.dialogs.compressSources", { count: props.entries.length }) }}
        </span>
        <NButton :disabled="files.acting" @click="emit('update:show', false)">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="primary"
          :loading="files.acting"
          :disabled="!valid"
          @click="() => void submit()"
        >
          <template #icon>
            <Archive :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("files.actions.compress") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.hint {
  margin: 0;
}

.archive-progress {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  margin-top: var(--space-3);
}

.dialog-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.dialog-foot .grow {
  flex: 1 1 auto;
}
</style>
