<script lang="ts">
let nextErrorId = 0;

function nextFolderSizeErrorId(): number {
  nextErrorId += 1;
  return nextErrorId;
}
</script>

<script setup lang="ts">
import { Calculator } from "@lucide/vue";
import { NButton, NSpin } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import {
  cancelFolderSize,
  folderSizeState,
  requestFolderSize,
} from "@/features/files/composables/useFolderSize";
import type { FilesEntry } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { formatBytes } from "@/shared/utils/format";

const props = defineProps<{ entry: FilesEntry; path: string }>();

const { t } = useI18n();
const files = useFilesStore();
const errorId = `fm-size-error-${nextFolderSizeErrorId()}`;

const state = computed(() =>
  folderSizeState(files.serverId, files.rootName, props.path),
);

const resultText = computed(() =>
  state.value.truncated
    ? `≥ ${formatBytes(state.value.bytes)}`
    : formatBytes(state.value.bytes),
);

const resultLabel = computed(() =>
  state.value.truncated
    ? `${t("files.browser.sizeResultAria", { name: props.entry.name, size: resultText.value })} — ${t("files.browser.sizePartialHint")}`
    : t("files.browser.sizeResultAria", {
        name: props.entry.name,
        size: resultText.value,
      }),
);

const announcement = computed<string>(() => {
  switch (state.value.status) {
    case "loading":
      return t("files.browser.sizeAnnounceLoading", { name: props.entry.name });
    case "done":
      return resultLabel.value;
    case "error":
      return t("files.browser.sizeAnnounceError", {
        name: props.entry.name,
        error: state.value.error ?? "",
      });
    default:
      return "";
  }
});

function calculate(): void {
  void requestFolderSize(
    files.serverId,
    files.rootName,
    props.path,
  );
}

function cancel(): void {
  cancelFolderSize(files.serverId, files.rootName, props.path);
}
</script>

<template>
  <span class="fm-size">
    <template v-if="state.status === 'loading'">
      <NSpin :size="12" aria-hidden="true" />
      <NButton
        text
        size="tiny"
        class="fm-size-action"
        :aria-label="t('files.browser.sizeCancelAria', { name: entry.name })"
        @click="cancel"
      >
        {{ t("files.actions.cancel") }}
      </NButton>
    </template>
    <template v-else-if="state.status === 'done'">
      <span class="fm-size-value" :title="resultLabel">{{ resultText }}</span>
      <span v-if="state.truncated" class="fm-size-partial">
        {{ t("files.browser.sizePartial") }}
      </span>
    </template>
    <NButton
      v-else-if="state.status === 'error'"
      text
      size="tiny"
      class="fm-size-action fm-size-action--error"
      :title="state.error ?? t('files.browser.sizeErrorHint')"
      :aria-label="t('files.browser.sizeRetryAria', { name: entry.name })"
      :aria-describedby="errorId"
      @click="calculate"
    >
      {{ t("files.browser.sizeFailed") }}
    </NButton>
    <NButton
      v-else
      text
      size="tiny"
      class="fm-size-action fm-size-action--icon"
      :aria-label="t('files.browser.sizeCalculateAria', { name: entry.name })"
      @click="calculate"
    >
      <template #icon>
        <Calculator :size="14" :stroke-width="1.8" aria-hidden="true" />
      </template>
    </NButton>
    <span class="fm-sr-only" role="status" aria-live="polite">{{
      announcement
    }}</span>
    <span
      v-if="state.status === 'error' && state.error"
      :id="errorId"
      class="fm-sr-only"
    >
      {{ state.error }}
    </span>
  </span>
</template>

<style scoped>
.fm-size {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  font-variant-numeric: tabular-nums;
}

.fm-size-action {
  color: var(--success-ink);
  font-family: var(--font-body);
  font-size: var(--text-xs);
}

.fm-size-action:hover {
  color: var(--accent-ink-hover);
  text-decoration: underline;
}

.fm-size-action--icon:hover {
  text-decoration: none;
}

.fm-size-action--error {
  color: var(--danger-ink);
}

.fm-size-value {
  color: var(--muted);
}

.fm-size-partial {
  color: var(--muted);
  font-style: italic;
}

.fm-sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
</style>
