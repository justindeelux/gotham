<script setup lang="ts">
import { NButton } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import TerminalSurface from "@/features/terminal/components/TerminalSurface.vue";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import { useTerminalWindowStore } from "@/features/terminal/stores/terminalWindow";

/**
 * Page-mode host for one terminal session (JUS-245).
 *
 * Later entry points (the standalone page and the server detail tab, JUS-246)
 * call `useTerminalStore().openPageSession(...)` and mount this component with
 * the returned id. When the session was detached, the pane renders a notice
 * instead of a surface: there is no window → page action and no duplicate
 * shell is created on return.
 */
interface Props {
  tabId: string;
}

const props = defineProps<Props>();
/**
 * startNew is emitted only from the moved-to-window notice. The host owns the
 * decision and opens a fresh page shell; the pane never creates a session on
 * its own.
 */
const emit = defineEmits<{ startNew: [] }>();
const { t } = useI18n();
const terminal = useTerminalStore();
const windowStore = useTerminalWindowStore();

const tab = computed(() => terminal.findTab(props.tabId));
const detached = computed<boolean>(() => tab.value?.mode === "window");

/** showWindow brings the floating window forward for a detached session. */
function showWindow(): void {
  windowStore.setMinimized(false);
  if (tab.value) {
    terminal.activate(tab.value.id);
  }
}

/** closeDetached closes through the shared confirmation path. */
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
      <div class="term-detached-actions">
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
  /* The one-way notice must stay reachable when the floating window opens
   * over the page (JUS-246 r2): paint it above the window's chrome and give it
   * a surface so its buttons are real pointer targets, not hidden behind the
   * terminal. */
  position: relative;
  z-index: 74;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--elev-raised);
}

.term-detached-title {
  margin: 0;
  font-size: var(--text-lg);
  color: var(--fg-2);
}

.term-detached-actions {
  display: flex;
  gap: var(--space-2);
  justify-content: center;
}
</style>
