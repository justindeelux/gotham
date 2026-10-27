<script setup lang="ts">
import { NButton, NModal } from "naive-ui";
import { useMessage } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { useFilesPane } from "@/features/files/stores/filesPane";

const { t } = useI18n();
const message = useMessage();
const files = useFilesPane().files;

const show = computed<boolean>(() => files.conflictPrompt !== null);
const names = computed<string[]>(() => files.conflictPrompt?.names ?? []);
const mode = computed<"move" | "copy">(() => files.conflictPrompt?.op ?? "copy");

async function replace(): Promise<void> {
  const summary = await files.resolveConflictPrompt(true);
  message.success(
    String(
      t(
        mode.value === "move" ? "files.dialogs.moved" : "files.dialogs.copied",
        { count: summary.done.length },
      ),
    ),
  );
}

async function skip(): Promise<void> {
  await files.resolveConflictPrompt(false);
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="show"
    preset="card"
    style="width: 460px; max-width: 94vw"
    :title="t('files.dialogs.conflictTitle')"
    @update:show="(value: boolean) => { if (!value) void skip(); }"
  >
    <p class="small">{{ t("files.dialogs.conflictMessage", { count: names.length }) }}</p>
    <ul class="names mono">
      <li v-for="name in names" :key="name">{{ name }}</li>
    </ul>
    <p class="small muted">{{ t("files.dialogs.conflictHint") }}</p>
    <template #footer>
      <div class="dialog-foot">
        <NButton @click="() => void skip()">
          {{ t("files.actions.skip") }}
        </NButton>
        <NButton type="primary" :loading="files.acting" @click="() => void replace()">
          {{ t("files.actions.replace") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.small {
  margin: 0;
}

.names {
  margin: var(--space-2) 0;
  padding-left: var(--space-4);
  max-height: 180px;
  overflow-y: auto;
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}
</style>
