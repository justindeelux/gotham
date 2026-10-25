<script setup lang="ts">
import { NButton, NModal, NPopconfirm, useMessage } from "naive-ui";
import { computed, nextTick, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useEditorStore } from "@/features/files/stores/editor";

const { t } = useI18n();
const message = useMessage();
const editor = useEditorStore();

const show = computed<boolean>(() => editor.conflictTab !== null);
const tab = computed(() => editor.conflictTab);
const name = computed<string>(() => editor.conflictTab?.name ?? "");
const baseEtag = computed<string>(() => editor.conflictTab?.etag ?? "");
const dirty = computed<boolean>(() => {
  const current = editor.conflictTab;
  return current !== null && editor.isDirty(current);
});

watch(show, async (open) => {
  if (!open) {
    return;
  }
  await nextTick();
  document
    .querySelector<HTMLElement>(".files-conflict-modal")
    ?.setAttribute("role", "alertdialog");
});

async function reload(): Promise<void> {
  const current = editor.conflictTab;
  if (!current) {
    return;
  }
  await editor.reload(current.id);
}

async function overwrite(): Promise<void> {
  const current = editor.conflictTab;
  if (!current) {
    return;
  }
  const outcome = await editor.overwrite(current.id);
  if (outcome === "saved") {
    message.success(String(t("files.editor.saved", { name: current.name })));
  } else if (outcome === "error") {
    message.error(current.error || String(t("files.editor.saveFailed", { name: current.name })));
  }
}

function saveAs(): void {
  const current = editor.conflictTab;
  if (current) {
    editor.openSaveAs(current.id);
  }
}
</script>

<template>
  <NModal
    class="app-modal files-conflict-modal"
    :show="show"
    preset="card"
    style="width: 480px; max-width: 94vw"
    :title="t('files.editor.conflictTitle')"
    @update:show="(value: boolean) => { if (!value) editor.dismissConflict(); }"
  >
    <p class="small">{{ t("files.editor.conflictMessage", { name }) }}</p>
    <dl class="conflict-details">
      <dt>{{ t("files.editor.conflictPath") }}</dt>
      <dd class="mono">{{ tab?.path }}</dd>
      <dt>{{ t("files.editor.conflictBase") }}</dt>
      <dd class="mono">{{ baseEtag }}</dd>
    </dl>
    <template #footer>
      <div class="dialog-foot">
        <NPopconfirm
          v-if="dirty"
          :positive-text="t('files.editor.conflictReloadConfirm')"
          @positive-click="() => void reload()"
        >
          <template #trigger>
            <NButton>{{ t("files.editor.conflictReload") }}</NButton>
          </template>
          {{ t("files.editor.conflictReloadWarning") }}
        </NPopconfirm>
        <NButton v-else @click="() => void reload()">
          {{ t("files.editor.conflictReload") }}
        </NButton>
        <NButton @click="saveAs">
          {{ t("files.editor.conflictSaveAs") }}
        </NButton>
        <NPopconfirm
          :positive-text="t('files.editor.conflictOverwriteConfirm')"
          @positive-click="() => void overwrite()"
        >
          <template #trigger>
            <NButton type="error" :loading="tab?.saving">
              {{ t("files.editor.conflictOverwrite") }}
            </NButton>
          </template>
          {{ t("files.editor.conflictOverwriteWarning") }}
        </NPopconfirm>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.small {
  margin: 0;
}

.conflict-details {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 4px var(--space-3);
  margin: var(--space-3) 0 0;
  font-size: var(--text-xs);
}

.conflict-details dt {
  color: var(--muted);
}

.conflict-details dd {
  margin: 0;
  overflow-wrap: anywhere;
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
  flex-wrap: wrap;
}
</style>
