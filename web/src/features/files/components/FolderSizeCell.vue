<script lang="ts">
/**
 * nextErrorId numbers each cell's aria-describedby target. It lives at module
 * scope, not in setup: a counter inside <script setup> restarts at 0 for every
 * instance, so every row would reuse `fm-size-error-1` and the ids would clash.
 */
let nextErrorId = 0;

/** nextFolderSizeErrorId hands out the next unique error-id suffix. */
function nextFolderSizeErrorId(): number {
  nextErrorId += 1;
  return nextErrorId;
}
</script>

<script setup lang="ts">
import { Calculator } from "@lucide/vue";
import { NButton, NSpin } from "naive-ui";
import { computed } from "vue";import { useI18n } from "vue-i18n";

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
 * quiet icon-only "Calculate" button, a spinner with a Cancel control while the walk runs, the
 * result (with a "partial" marker when the agent stopped at its entry cap), or
 * a retry affordance after a failure. The state lives in `useFolderSize`, keyed
 * by server/root/path, so it survives the list's row windowing.
 *
 * The cell's `.fm-size` wrapper is one of the browser's row controls
 * (`isRowControl`), so a press or key on Calculate/Cancel/Retry never reaches
 * the row's selection handler and the buttons keep their native Enter/Space.
 */
const props = defineProps<{ entry: FilesEntry; path: string }>();

const { t } = useI18n();
const files = useFilesStore();
const teams = useTeamsStore();
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
    : t("files.browser.sizeResultAria", { name: props.entry.name, size: resultText.value }),
);

/**
 * announcement feeds the persistent polite live region, so a screen reader
 * hears the completion or failure without the user hunting for the cell.
 */
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
      quaternary
      size="tiny"
      class="fm-size-action"
      :aria-label="t('files.browser.sizeCalculateAria', { name: entry.name })"
      @click="calculate"
    >
      <template #icon>
        <Calculator :size="14" :stroke-width="1.8" aria-hidden="true" />
      </template>
    </NButton>
    <!-- Persistent live region: it exists in every state so a status change is
         announced (a live region inserted already-full is not). -->
    <span class="fm-sr-only" role="status" aria-live="polite">{{ announcement }}</span>
    <span v-if="state.status === 'error' && state.error" :id="errorId" class="fm-sr-only">
      {{ state.error }}
    </span>
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
  /* JUS-206/216: the Calculate control is a quiet icon button (no background);
     the Cancel/Retry text controls keep the reference green body-text link. */
  color: var(--success-ink);
  font-family: var(--font-body);
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

/* Clipped out of view but still in the accessibility tree (mirrors the
   browser's shared .fm-sr-only). */
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
