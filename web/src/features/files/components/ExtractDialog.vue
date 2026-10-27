<script setup lang="ts">
import { ArchiveRestore } from "@lucide/vue";
import { NButton, NCheckbox, NModal, NProgress, useMessage } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { describeFilesError } from "@/features/files/api/files";
import type { FilesEntry } from "@/features/files/schemas/files";
import { useFilesPane } from "@/features/files/stores/filesPane";

interface Props {
  show: boolean;
  entry: FilesEntry | null;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
}>();

const { t } = useI18n();
const message = useMessage();
const files = useFilesPane().files;

const overwrite = ref(false);

watch(
  () => props.show,
  (open) => {
    if (open) {
      overwrite.value = false;
    }
  },
);

const name = computed<string>(() => props.entry?.name ?? "");

async function submit(): Promise<void> {
  if (!props.entry || files.acting) {
    return;
  }
  try {
    await files.extract(props.entry, overwrite.value);
    message.success(String(t("files.dialogs.extracted", { name: name.value })));
    emit("update:show", false);
  } catch (err) {
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
    :title="t('files.dialogs.extractTitle')"
    :mask-closable="!files.acting"
    :close-on-esc="!files.acting"
    :closable="!files.acting"
    @update:show="(value: boolean) => !files.acting && emit('update:show', value)"
  >
    <p class="message">
      {{ t("files.dialogs.extractMessage", { name }) }}
    </p>
    <NCheckbox v-model:checked="overwrite">
      {{ t("files.dialogs.extractOverwrite") }}
    </NCheckbox>
    <p class="hint small muted">{{ t("files.dialogs.extractHint") }}</p>

    <div v-if="files.acting" class="archive-progress">
      <NProgress
        type="line"
        :percentage="100"
        :processing="true"
        :show-indicator="false"
        :height="6"
      />
      <span class="small muted" role="status" aria-live="polite">{{ t("files.dialogs.extractProgress") }}</span>
    </div>

    <template #footer>
      <div class="dialog-foot">
        <span class="grow small muted">{{ t("files.dialogs.extractDestination") }}</span>
        <NButton :disabled="files.acting" @click="emit('update:show', false)">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="primary"
          :loading="files.acting"
          :disabled="props.entry === null"
          @click="() => void submit()"
        >
          <template #icon>
            <ArchiveRestore :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("files.actions.extract") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.message {
  margin: 0 0 var(--space-3);
}

.hint {
  margin: var(--space-2) 0 0;
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
