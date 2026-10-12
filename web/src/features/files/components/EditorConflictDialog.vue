<script setup lang="ts">
import { NButton, NModal, useMessage } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { useEditorStore } from "@/features/files/stores/editor";
import { useTeamsStore } from "@/features/teams";

const { t } = useI18n();
const message = useMessage();
const editor = useEditorStore();
const teams = useTeamsStore();

/** show follows the store's pending conflict prompt. */
const show = computed<boolean>(() => editor.conflictTab !== null);
const name = computed<string>(() => editor.conflictTab?.name ?? "");

/** reload discards local edits and re-reads the server's version. */
async function reload(): Promise<void> {
  const tab = editor.conflictTab;
  if (!tab) {
    return;
  }
  await editor.reload(tab.id, teams.activeTeamId);
}

/** overwrite force-saves the buffer over the concurrent change. */
async function overwrite(): Promise<void> {
  const tab = editor.conflictTab;
  if (!tab) {
    return;
  }
  const outcome = await editor.overwrite(tab.id, teams.activeTeamId);
  if (outcome === "saved") {
    message.success(String(t("files.editor.saved", { name: tab.name })));
  } else if (outcome === "error") {
    message.error(tab.error || String(t("files.editor.saveFailed", { name: tab.name })));
  }
}

/** saveAs opens the save-as-copy dialog for the conflicting tab. */
function saveAs(): void {
  const tab = editor.conflictTab;
  if (tab) {
    editor.openSaveAs(tab.id);
  }
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="show"
    preset="card"
    style="width: 480px; max-width: 94vw"
    :title="t('files.editor.conflictTitle')"
    @update:show="(value: boolean) => { if (!value) editor.dismissConflict(); }"
  >
    <p class="small">{{ t("files.editor.conflictMessage", { name }) }}</p>
    <template #footer>
      <div class="dialog-foot">
        <NButton @click="() => void reload()">
          {{ t("files.editor.conflictReload") }}
        </NButton>
        <NButton @click="saveAs">
          {{ t("files.editor.conflictSaveAs") }}
        </NButton>
        <NButton type="primary" :loading="editor.conflictTab?.saving" @click="() => void overwrite()">
          {{ t("files.editor.conflictOverwrite") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.small {
  margin: 0;
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
  flex-wrap: wrap;
}
</style>
