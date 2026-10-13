<script setup lang="ts">
import { NAlert, NButton, NModal } from "naive-ui";
import { useMessage } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { FilesEntry } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import type { TransferSummary } from "@/features/files/stores/files";

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

const summary = ref<TransferSummary | null>(null);

watch(
  () => props.show,
  (open) => {
    if (open) {
      summary.value = null;
    }
  },
);

const hasFolders = computed<boolean>(() =>
  props.entries.some((entry) => entry.kind === "directory"),
);

/** hasSymlink reassures that deleting a link never removes its target. */
const hasSymlink = computed<boolean>(() =>
  props.entries.some((entry) => entry.kind === "symlink"),
);

/** title names the target kind (folder vs file). */
const title = computed<string>(() =>
  String(t(hasFolders.value ? "files.dialogs.deleteFolderTitle" : "files.dialogs.deleteTitle")),
);

/**
 * messageText explains the outcome: a delete always moves the entries to the
 * recycle bin, where they stay recoverable until the bin is emptied. Permanent
 * deletion is offered only from the bin itself.
 */
const messageText = computed<string>(() =>
  String(
    t(
      hasFolders.value
        ? "files.dialogs.recycleFolderMessage"
        : "files.dialogs.recycleMessage",
    ),
  ),
);

/** submit moves every entry to the recycle bin and reports a per-name outcome. */
async function submit(): Promise<void> {
  if (files.acting) {
    return;
  }
  try {
    // Every entry the dialog is open for is deletable: a folder recursively, a
    // file or symlink with a plain unlink (a symlink removes the link, never
    // its target). Special rows never reach this dialog.
    const result = await files.remove(props.entries);
    if (result.failed.length === 0) {
      message.success(
        String(
          t("files.dialogs.movedToBin", { count: result.done.length }),
        ),
      );
      emit("update:show", false);
      return;
    }
    summary.value = result;
    message.warning(
      String(
        t("files.dialogs.deletePartial", {
          done: result.done.length,
          failed: result.failed.length,
        }),
      ),
    );
  } catch {
    // runMutation only throws if the refresh itself failed; the listing keeps
    // the entries and the browser shows the read error inline.
    message.error(String(t("common.errors.unexpected")));
  }
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="props.show"
    preset="card"
    style="width: 460px; max-width: 94vw"
    :title="title"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <p class="small">{{ messageText }}</p>
    <ul class="names mono">
      <li v-for="entry in props.entries" :key="entry.name">{{ entry.name }}</li>
    </ul>
    <p v-if="hasSymlink" class="small muted symlink-hint">
      {{ t("files.dialogs.recycleSymlinkHint") }}
    </p>
    <p class="small muted">{{ t("files.dialogs.recycleHint") }}</p>

    <NAlert v-if="summary" class="mt-3" type="warning" :title="t('files.dialogs.deletePartialTitle')">
      <ul class="names mono">
        <li v-for="failure in summary.failed" :key="failure.name">
          {{ failure.name }} — {{ failure.error }}
        </li>
      </ul>
    </NAlert>

    <template #footer>
      <div class="dialog-foot">
        <NButton @click="emit('update:show', false)">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="warning"
          :loading="files.acting"
          @click="() => void submit()"
        >
          {{ t("files.dialogs.recycleConfirm") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.names {
  margin: var(--space-2) 0;
  padding-left: var(--space-4);
  max-height: 180px;
  overflow-y: auto;
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.small {
  margin: 0;
}

.symlink-hint {
  margin-top: var(--space-1);
}

.mt-3 {
  margin-top: var(--space-3);
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}
</style>
