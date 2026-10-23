<script setup lang="ts">
import { NButton, NModal } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { useTerminalStore } from "@/features/terminal/stores/terminal";

const emit = defineEmits<{ confirmed: [] }>();
const { t } = useI18n();
const terminal = useTerminalStore();

const show = computed<boolean>(() => terminal.closeRequest !== null);
const closing = computed(() => {
  const ids = terminal.closeRequest?.ids ?? [];
  return ids
    .map((id) => terminal.findTab(id))
    .filter((tab) => tab !== null);
});
const multi = computed<boolean>(() => closing.value.length > 1);

function cancel(): void {
  terminal.cancelClose();
}

function confirm(): void {
  terminal.confirmClose();
  emit("confirmed");
}
</script>

<template>
  <NModal
    class="app-modal term-close-modal"
    :show="show"
    preset="card"
    style="width: min(30rem, 94vw)"
    :title="multi ? t('terminal.close.titleWindow') : t('terminal.close.titleTab')"
    :mask-closable="false"
    @update:show="(value: boolean) => { if (!value) cancel(); }"
  >
    <p v-if="multi" class="small">{{ t("terminal.close.messageWindow") }}</p>
    <p v-else class="small">
      {{
        t("terminal.close.messageTab", {
          title: closing[0]?.title ?? t("terminal.window.heading"),
        })
      }}
    </p>
    <ul class="term-close-list" :aria-label="t('terminal.close.listLabel')">
      <li v-for="tab in closing" :key="tab.id" class="mono">
        {{ tab.title }}
        <span class="term-close-meta">
          {{ tab.target }}<template v-if="tab.cwd"> · {{ tab.cwd }}</template>
        </span>
      </li>
    </ul>
    <template #footer>
      <div class="dialog-foot">
        <NButton @click="cancel">{{ t("terminal.close.cancel") }}</NButton>
        <NButton type="error" @click="confirm">
          {{ multi ? t("terminal.close.confirmAll") : t("terminal.close.confirm") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.dialog-foot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-2);
}

.term-close-list {
  margin: var(--space-3) 0 0;
  padding-left: var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  max-height: min(14rem, 40vh);
  overflow-y: auto;
  font-size: var(--text-sm);
}

.term-close-meta {
  color: var(--meta);
  font-family: var(--font-body);
  font-size: var(--text-xs);
  margin-left: var(--space-2);
}
</style>
