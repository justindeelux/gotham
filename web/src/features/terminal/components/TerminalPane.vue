<script setup lang="ts">
import { NButton } from "naive-ui";
import { computed, nextTick, watch } from "vue";
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
/**
 * windowMinimized decides which moved state the pane shows (JUS-255).
 *
 * The floating window always owns the stacking order: a page pane can never
 * paint above it, so an interactive notice in the pane would either be hidden
 * behind the terminal or (the JUS-246 regression) be lifted over it and cover
 * the shell. The pane therefore offers its actions only while the window is
 * collapsed — that is exactly when the actions are the visible way back — and
 * shows a passive line while the window is open on screen.
 */
const windowMinimized = computed<boolean>(() => windowStore.minimized);

// A one-way transfer unmounts the surface that owned the trigger, so focus
// would fall to document.body. Hand it to the window tab instead (the same
// target minimize/restore use). Mounting an already detached pane — returning
// to the source route — does not steal focus.
watch(detached, (moved, previous) => {
  if (!moved || previous) {
    return;
  }
  void nextTick(() => {
    document.getElementById(`term-tab-${props.tabId}`)?.focus();
  });
});

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
  /* In-flow page content, never an overlay: the floating window owns the
   * stacking order (JUS-255), so this card must not carry a z-index. */
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
