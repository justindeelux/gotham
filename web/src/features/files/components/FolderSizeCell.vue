<script setup lang="ts">
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
import { useTeamsStore } from "@/features/teams";
import { formatBytes } from "@/shared/utils/format";

/**
 * FolderSizeCell (JUS-162) renders the Size cell of ONE folder row: a
 * "Calculate" link, a spinner with a Cancel control while the walk runs, the
 * result (with a "partial" marker when the agent stopped at its entry cap), or
 * a retry affordance after a failure. The state lives in `useFolderSize`, keyed
 * by server/root/path, so it survives the list's row windowing.
 */
const props = defineProps<{ entry: FilesEntry; path: string }>();

const { t } = useI18n();
const files = useFilesStore();
const teams = useTeamsStore();

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
    : t("files.browser.sizeResultAria", { name: props.entry.name, size: resultText.value }),
);

function calculate(): void {
  void requestFolderSize(files.serverId, teams.activeTeamId, files.rootName, props.path);
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
      <span class="fm-size-value" :aria-label="resultLabel" :title="resultLabel">
        {{ resultText }}
      </span>
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
      @click="calculate"
    >
      {{ t("files.browser.sizeFailed") }}
    </NButton>
    <NButton
      v-else
      text
      size="tiny"
      class="fm-size-action"
      :aria-label="t('files.browser.sizeCalculateAria', { name: entry.name })"
      @click="calculate"
    >
      {{ t("files.browser.sizeCalculate") }}
    </NButton>
  </span>
</template>

<style scoped>
/* The cell keeps the browser's mono/xs grid; only the link color and the
   spinner gap are set here, from tokens (no hard-coded values). */
.fm-size {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  font-variant-numeric: tabular-nums;
}

.fm-size-action {
  color: var(--link);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
}

.fm-size-action:hover {
  color: var(--accent-ink-hover);
  text-decoration: underline;
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
</style>
