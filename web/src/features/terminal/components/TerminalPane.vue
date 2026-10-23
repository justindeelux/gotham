<script setup lang="ts">
import { NButton } from "naive-ui";
import { computed, nextTick, watch } from "vue";
import { useI18n } from "vue-i18n";

import TerminalSurface from "@/features/terminal/components/TerminalSurface.vue";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import { useTerminalWindowStore } from "@/features/terminal/stores/terminalWindow";

interface Props {
  tabId: string;
}

const props = defineProps<Props>();
const emit = defineEmits<{ startNew: [] }>();
const { t } = useI18n();
const terminal = useTerminalStore();
const windowStore = useTerminalWindowStore();

const tab = computed(() => terminal.findTab(props.tabId));
const detached = computed<boolean>(() => tab.value?.mode === "window");
const windowMinimized = computed<boolean>(() => windowStore.minimized);

watch(detached, (moved, previous) => {
  if (!moved || previous) {
    return;
  }
  void nextTick(() => {
    document.getElementById(`term-tab-${props.tabId}`)?.focus();
  });
});

function showWindow(): void {
  windowStore.setMinimized(false);
  if (tab.value) {
    terminal.activate(tab.value.id);
  }
}

function closeDetached(): void {
  if (tab.value) {
    terminal.requestClose([tab.value.id]);
  }
}
</script>

<template>
  <section v-if="tab" class="term-pane">
    <div v-if="detached" class="term-detached" role="status">
      <h2 class="term-detached-title">{{ t("terminal.pane.movedTitle") }}</h2>
      <p class="small muted">{{ t("terminal.pane.movedBody") }}</p>
      <div v-if="windowMinimized" class="term-detached-actions">
        <NButton size="small" type="primary" @click="showWindow">
          {{ t("terminal.pane.showWindow") }}
        </NButton>
        <NButton size="small" secondary @click="emit('startNew')">
          {{ t("terminal.actions.retry") }}
        </NButton>
        <NButton size="small" quaternary @click="closeDetached">
          {{ t("terminal.pane.close") }}
        </NButton>
      </div>
      <p v-else class="small muted term-detached-hint">
        {{ t("terminal.pane.movedOpenHint") }}
      </p>
    </div>
    <TerminalSurface v-else :tab-id="tab.id" variant="page" />
  </section>
</template>

<style scoped>
.term-pane {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  height: 100%;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: hidden;
  background: var(--surface);
}

.term-detached {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  max-width: min(33rem, 100%);
  margin: auto;
  padding: var(--space-6);
  text-align: center;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}

.term-detached-title {
  margin: 0;
  font-size: var(--text-lg);
  color: var(--fg-2);
}

.term-detached-hint {
  margin: 0;
}

.term-detached-actions {
  display: flex;
  gap: var(--space-2);
  justify-content: center;
}
</style>
