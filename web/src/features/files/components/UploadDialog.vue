<script setup lang="ts">
import { CloudDownload, File, Upload } from "@lucide/vue";
import { NButton, NModal, NProgress, NTag, useMessage } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { UploadItem } from "@/features/files/composables/useFileUpload";
import { useFileUpload } from "@/features/files/composables/useFileUpload";
import { useFilesStore } from "@/features/files/stores/files";
import { filesFromDrop, isFileDrag } from "@/features/files/utils/files";
import { useTeamsStore } from "@/features/teams";
import { formatBytes } from "@/shared/utils/format";

interface Props {
  show: boolean;
  /** Files dropped onto the browser, queued as soon as the dialog opens. */
  initialFiles: File[];
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
}>();

const { t } = useI18n();
const message = useMessage();
const files = useFilesStore();
const teams = useTeamsStore();
const upload = useFileUpload();

const dragging = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);

/** target is the directory the queue uploads into (frozen while open). */
const target = computed(() => ({
  serverId: files.serverId,
  teamId: teams.activeTeamId,
  root: files.rootName,
  path: files.path,
}));

watch(
  () => props.show,
  (open) => {
    if (open) {
      if (props.initialFiles.length > 0) {
        upload.addFiles(props.initialFiles);
      }
    } else {
      upload.clear();
    }
  },
);

/** picked queues the files a user chose through the native picker. */
function picked(event: Event): void {
  const input = event.target as HTMLInputElement;
  if (input.files) {
    upload.addFiles(input.files);
  }
  input.value = "";
}

/** dropped queues files dropped on the drop zone (folders are rejected). */
function dropped(event: DragEvent): void {
  dragging.value = false;
  if (!isFileDrag(event.dataTransfer?.types)) {
    return;
  }
  const { files: dropped, folders } = filesFromDrop(event.dataTransfer);
  if (folders > 0) {
    message.warning(String(t("files.dialogs.foldersUnsupported")));
  }
  if (dropped.length > 0) {
    upload.addFiles(dropped);
  }
}

/** close ends the batch, refreshing the listing when anything landed. */
function close(): void {
  if (upload.summary.value.done > 0) {
    void files.refresh();
  }
  emit("update:show", false);
}

/** start sends the queued files. */
async function start(): Promise<void> {
  await upload.start(target.value);
  if (upload.summary.value.done > 0) {
    void files.refresh();
  }
}

/** percent reads one item's progress as 0–100. */
function percent(item: UploadItem): number {
  if (item.status === "done") {
    return 100;
  }
  if (item.total <= 0) {
    return 0;
  }
  return Math.min(99, Math.round((item.loaded / item.total) * 100));
}

/** statusType maps an item's state onto an NProgress status. */
function statusType(item: UploadItem): "default" | "success" | "error" | "warning" {
  if (item.status === "done") {
    return "success";
  }
  if (item.status === "conflict") {
    return "warning";
  }
  if (item.status === "error") {
    return "error";
  }
  return "default";
}

/** statusLabel renders one item's state. */
function statusLabel(item: UploadItem): string {
  switch (item.status) {
    case "queued":
      return String(t("files.upload.waiting"));
    case "uploading":
      return String(t("files.upload.uploading", { percent: percent(item) }));
    case "done":
      return String(t("files.upload.done"));
    case "conflict":
      return String(t("files.upload.conflict"));
    case "cancelled":
      return String(t("files.upload.cancelled"));
    case "error":
    default:
      return item.message || String(t("files.upload.error"));
  }
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="props.show"
    preset="card"
    style="width: 620px; max-width: 94vw"
    :title="t('files.dialogs.uploadTitle')"
    @update:show="(value: boolean) => { if (!value) close(); }"
  >
    <button
      type="button"
      class="drop"
      :class="{ 'is-dragging': dragging }"
      @click="fileInput?.click()"
      @dragover.prevent="dragging = true"
      @dragleave.prevent="dragging = false"
      @drop.prevent="dropped"
    >
      <CloudDownload :size="26" :stroke-width="1.6" aria-hidden="true" />
      <span class="drop-title">{{ t("files.dialogs.uploadDrop") }}</span>
      <span class="small muted">{{ t("files.dialogs.uploadHint") }}</span>
    </button>
    <input
      ref="fileInput"
      type="file"
      multiple
      class="visually-hidden"
      @change="picked"
    />

    <div v-if="upload.items.value.length > 0" class="queue">
      <div v-for="item in upload.items.value" :key="item.id" class="row">
        <File :size="15" :stroke-width="1.7" aria-hidden="true" />
        <span class="name mono" :title="item.file.name">{{ item.file.name }}</span>
        <span class="meter">
          <NProgress
            type="line"
            :percentage="percent(item)"
            :status="statusType(item)"
            :show-indicator="false"
            :height="6"
          />
        </span>
        <span class="state small muted">
          <NTag v-if="item.status === 'conflict'" size="small" type="warning">
            {{ t("files.dialogs.nameExists") }}
          </NTag>
          <span v-else>{{ statusLabel(item) }}</span>
        </span>
        <span class="size small muted">{{ formatBytes(item.file.size) }}</span>
        <span class="row-actions">
          <NButton
            v-if="item.status === 'uploading'"
            size="tiny"
            quaternary
            @click="upload.cancel(item.id)"
          >
            {{ t("files.actions.cancel") }}
          </NButton>
          <NButton
            v-else-if="item.status === 'queued'"
            size="tiny"
            quaternary
            @click="upload.removeItem(item.id)"
          >
            {{ t("files.actions.cancel") }}
          </NButton>
          <NButton
            v-else-if="item.status === 'conflict'"
            size="tiny"
            secondary
            @click="() => void upload.retry(item.id, target, true)"
          >
            {{ t("files.actions.replace") }}
          </NButton>
          <NButton
            v-else-if="item.status === 'error'"
            size="tiny"
            secondary
            @click="() => void upload.retry(item.id, target, false)"
          >
            {{ t("common.actions.retry") }}
          </NButton>
        </span>
      </div>
    </div>
    <p v-else class="small muted empty">{{ t("files.dialogs.uploadEmpty") }}</p>

    <template #footer>
      <div class="dialog-foot">
        <span class="grow small muted">
          {{ t("files.upload.summary", {
            done: upload.summary.value.done,
            failed: upload.summary.value.error + upload.summary.value.conflict,
          }) }}
        </span>
        <NButton
          v-if="upload.hasFailures.value > 0"
          secondary
          :disabled="upload.uploading.value"
          @click="() => void upload.retryFailed(target)"
        >
          {{ t("files.dialogs.retryFailed") }}
        </NButton>
        <NButton @click="close">{{ t("common.actions.close") }}</NButton>
        <NButton
          type="primary"
          :loading="upload.uploading.value"
          :disabled="upload.pending.value === 0"
          @click="() => void start()"
        >
          <template #icon>
            <Upload :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
          {{ t("files.actions.upload") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.drop {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-6) var(--space-4);
  border: 1px dashed var(--border);
  border-radius: var(--radius-md);
  background: var(--surface-warm);
  color: var(--muted);
  cursor: pointer;
  transition: border-color var(--motion-base) var(--ease-standard);
}

.drop:hover,
.drop.is-dragging {
  border-color: var(--accent);
  color: var(--fg-2);
}

.drop-title {
  color: var(--fg-2);
  font-weight: 500;
}

.queue {
  margin-top: var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  max-height: 280px;
  overflow-y: auto;
}

.row {
  display: grid;
  grid-template-columns: 18px minmax(90px, 1.4fr) 120px minmax(80px, 1fr) 64px auto;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
}

.name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--fg-2);
}

.state {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.size {
  text-align: right;
}

.row-actions {
  display: inline-flex;
  justify-content: flex-end;
  min-width: 64px;
}

.empty {
  margin: var(--space-4) 0 0;
  text-align: center;
}

.dialog-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.dialog-foot .grow {
  flex: 1 1 auto;
}

.visually-hidden {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  border: 0;
}
</style>
