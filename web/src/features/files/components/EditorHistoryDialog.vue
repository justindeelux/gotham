<script setup lang="ts">
import { GitCompare, RotateCcw, Trash2 } from "@lucide/vue";
import { NButton, NEmpty, NModal, NPopconfirm, NSpin, useMessage } from "naive-ui";
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  deleteFileVersion,
  describeFilesError,
  listFileVersions,
} from "@/features/files/api/files";
import type { FilesVersion } from "@/features/files/schemas/files";
import { formatBytes, formatDate, relativeTime } from "@/shared/utils/format";

interface Props {
  show: boolean;
  serverId: string;
  root: string;
  path: string;
  fileName: string;
  dirty: boolean;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:show": [value: boolean];
  diff: [version: FilesVersion];
  restore: [version: FilesVersion];
}>();

const { t } = useI18n();
const message = useMessage();

const versions = ref<FilesVersion[]>([]);
const loading = ref(false);
const loadError = ref("");
const deletingId = ref("");

async function reload(): Promise<void> {
  loading.value = true;
  loadError.value = "";
  try {
    versions.value = await listFileVersions(
      props.serverId,
      props.root,
      props.path,
    );
  } catch (err) {
    versions.value = [];
    loadError.value = describeFilesError(err);
  } finally {
    loading.value = false;
  }
}

defineExpose({ reload });

watch(
  () => props.show,
  (show) => {
    if (show) {
      void reload();
    }
  },
  { immediate: true },
);

async function remove(version: FilesVersion): Promise<void> {
  if (deletingId.value) {
    return;
  }
  deletingId.value = version.id;
  try {
    await deleteFileVersion(
      props.serverId,
      props.root,
      props.path,
      version.id,
    );
    message.success(String(t("files.editor.historyDeleted")));
    await reload();
  } catch (err) {
    message.error(describeFilesError(err));
  } finally {
    deletingId.value = "";
  }
}

function close(): void {
  emit("update:show", false);
}

function sizeLabel(version: FilesVersion): string {
  return formatBytes(version.size);
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="show"
    preset="card"
    style="width: 720px; max-width: 94vw"
    :title="t('files.editor.historyTitle', { name: fileName })"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <NSpin :show="loading">
      <p v-if="loadError" class="small muted" role="alert">{{ loadError }}</p>
      <NEmpty
        v-else-if="versions.length === 0 && !loading"
        :description="t('files.editor.historyEmpty')"
      />
      <table v-else class="ed-history-table">
        <thead>
          <tr>
            <th scope="col">{{ t("files.editor.historyTime") }}</th>
            <th scope="col">{{ t("files.editor.historySize") }}</th>
            <th scope="col" class="ed-history-actions-head">
              {{ t("files.editor.historyActions") }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="version in versions" :key="version.id">
            <td :title="version.created_at">
              <span>{{ formatDate(version.created_at) }}</span>
              <span class="muted"> · {{ relativeTime(version.created_at, null, { long: true }) }}</span>
            </td>
            <td class="mono">{{ sizeLabel(version) }}</td>
            <td>
              <div class="ed-history-actions">
                <NButton size="tiny" secondary @click="emit('diff', version)">
                  <template #icon>
                    <GitCompare :size="14" :stroke-width="1.8" aria-hidden="true" />
                  </template>
                  {{ t("files.editor.historyDiff") }}
                </NButton>
                <NPopconfirm
                  :positive-text="t('files.editor.historyRestore')"
                  @positive-click="emit('restore', version)"
                >
                  <template #trigger>
                    <NButton size="tiny" secondary>
                      <template #icon>
                        <RotateCcw :size="14" :stroke-width="1.8" aria-hidden="true" />
                      </template>
                      {{ t("files.editor.historyRestore") }}
                    </NButton>
                  </template>
                  {{
                    dirty
                      ? t("files.editor.historyRestoreDirty")
                      : t("files.editor.historyRestoreConfirm")
                  }}
                </NPopconfirm>
                <NPopconfirm
                  :positive-text="t('files.editor.historyDelete')"
                  @positive-click="() => void remove(version)"
                >
                  <template #trigger>
                    <NButton
                      size="tiny"
                      quaternary
                      type="error"
                      :loading="deletingId === version.id"
                      :aria-label="t('files.editor.historyDelete')"
                    >
                      <template #icon>
                        <Trash2 :size="14" :stroke-width="1.8" aria-hidden="true" />
                      </template>
                    </NButton>
                  </template>
                  {{ t("files.editor.historyDeleteConfirm") }}
                </NPopconfirm>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </NSpin>
    <template #footer>
      <div class="dialog-foot">
        <NButton @click="close">{{ t("files.editor.historyClose") }}</NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.ed-history-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--text-sm);
}

.ed-history-table th {
  padding: var(--space-2);
  border-bottom: 1px solid var(--border);
  color: var(--muted);
  font-size: var(--text-xs);
  font-weight: 600;
  text-align: left;
}

.ed-history-table td {
  padding: var(--space-2);
  border-bottom: 1px solid var(--border);
  color: var(--fg-2);
  vertical-align: middle;
}

.ed-history-table tr:last-child td {
  border-bottom: none;
}

.ed-history-actions-head {
  text-align: right;
}

.ed-history-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-1);
  flex-wrap: wrap;
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}

.small {
  font-size: var(--text-sm);
}

.mono {
  font-family: var(--font-mono);
}
</style>
