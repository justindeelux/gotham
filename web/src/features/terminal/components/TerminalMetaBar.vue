<script setup lang="ts">
import { BookMarked, ExternalLink, Keyboard, X } from "@lucide/vue";
import { NButton, NPopover } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import QuickCommandPanel from "@/features/terminal/components/QuickCommandPanel.vue";
import TerminalThemeMenu from "@/features/terminal/components/TerminalThemeMenu.vue";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalTab } from "@/features/terminal/stores/terminal";
import {
  isContainerTerminalTarget,
  shortSessionId,
} from "@/features/terminal/utils/terminal";

/**
 * Terminal meta bar (JUS-249 extraction): target/cwd/session chips, the
 * shortcuts popover, the quick-command popover, the theme menu, and page-mode
 * actions.
 */
interface Props {
  tab: TerminalTab;
  variant: "page" | "window";
}

const props = defineProps<Props>();
const emit = defineEmits<{ inserted: [label: string] }>();
const { t } = useI18n();
const terminal = useTerminalStore();

const shortcutsOpen = ref(false);
const commandsOpen = ref(false);

const isContainer = computed<boolean>(() =>
  isContainerTerminalTarget(props.tab.target),
);

/**
 * targetText is the visible target identity. A container session shows the
 * server-resolved "service · container name" label; a host session keeps the
 * contract target. The label is server data shown on purpose, never a raw
 * backend error body.
 */
const targetText = computed<string>(() =>
  isContainer.value
    ? props.tab.targetLabel || props.tab.containerName || props.tab.target
    : props.tab.target,
);

/** detachToWindow is the one-way page → window transfer. */
function detachToWindow(): void {
  terminal.detachToWindow(props.tab.id);
}

/** closeTerminal uses the shared confirmation path. */
function closeTerminal(): void {
  terminal.requestClose([props.tab.id]);
}

/** onInserted closes the panel and lets the surface announce the insertion. */
function onInserted(label: string): void {
  commandsOpen.value = false;
  emit("inserted", label);
}
</script>

<template>
  <div class="term-meta">
    <span class="term-chip">
      <span class="term-key">{{ t("terminal.pane.target") }}</span>
      <span class="mono">{{ targetText }}</span>
    </span>
    <span
      v-if="isContainer && props.tab.applicationName"
      class="term-chip term-container"
      :title="t('terminal.pane.containerNote')"
    >
      <span class="term-key">{{ t("terminal.pane.application") }}</span>
      <span class="mono">{{ props.tab.applicationName }}</span>
    </span>
    <span v-if="props.tab.cwd" class="term-chip term-cwd" :title="props.tab.cwd">
      <span class="term-key">{{ t("terminal.pane.cwd") }}</span>
      <span class="mono">{{ props.tab.cwd }}</span>
    </span>
    <span
      v-if="props.tab.sessionId"
      class="term-session mono"
      :title="props.tab.sessionId"
    >
      {{ shortSessionId(props.tab.sessionId) }}
    </span>
    <NPopover
      v-model:show="shortcutsOpen"
      trigger="click"
      placement="bottom-end"
    >
      <template #trigger>
        <NButton
          quaternary
          size="tiny"
          class="term-shortcuts-btn"
          :aria-label="t('terminal.shortcuts.show')"
        >
          <template #icon>
            <Keyboard :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
        </NButton>
      </template>
      <div class="term-shortcuts">
        <p class="term-shortcuts-title">{{ t("terminal.shortcuts.title") }}</p>
        <dl>
          <div>
            <dt class="mono">Alt+Shift+W</dt>
            <dd>{{ t("terminal.shortcuts.closeWindow") }}</dd>
          </div>
          <div>
            <dt class="mono">Alt+M</dt>
            <dd>{{ t("terminal.shortcuts.minimize") }}</dd>
          </div>
          <div>
            <dt class="mono">Delete</dt>
            <dd>{{ t("terminal.shortcuts.closeTab") }}</dd>
          </div>
          <div>
            <dt class="mono">Ctrl+Shift+E</dt>
            <dd>{{ t("terminal.shortcuts.escape") }}</dd>
          </div>
          <div>
            <dt class="mono">Ctrl+Shift+Space</dt>
            <dd>{{ t("terminal.shortcuts.chooseSuggestion") }}</dd>
          </div>
        </dl>
        <p class="term-shortcuts-note small muted">
          {{ t("terminal.shortcuts.hint") }}
        </p>
      </div>
    </NPopover>
    <NPopover
      v-model:show="commandsOpen"
      trigger="click"
      placement="bottom-end"
      :duration="0"
      :animated="false"
    >
      <template #trigger>
        <NButton
          quaternary
          size="tiny"
          class="term-commands-btn"
          :aria-label="t('terminal.quickCommands.open')"
          @click.stop
        >
          <template #icon>
            <BookMarked :size="14" :stroke-width="1.8" aria-hidden="true" />
          </template>
        </NButton>
      </template>
      <QuickCommandPanel
        :tab-id="props.tab.id"
        :shell="props.tab.shell"
        :integration-detected="props.tab.integrationDetected"
        :alternate-screen="props.tab.alternateScreen"
        @inserted="onInserted"
      />
    </NPopover>
    <span class="grow" />
    <TerminalThemeMenu />
    <template v-if="props.variant === 'page'">
      <NButton
        size="tiny"
        secondary
        class="term-open-in-window"
        @click.stop="detachToWindow"
      >
        <template #icon>
          <ExternalLink :size="13" :stroke-width="1.8" aria-hidden="true" />
        </template>
        {{ t("terminal.pane.openInWindow") }}
      </NButton>
      <NButton
        quaternary
        size="tiny"
        :aria-label="t('terminal.pane.close')"
        @click.stop="closeTerminal"
      >
        <template #icon>
          <X :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
    </template>
  </div>
</template>

<style scoped>
.term-meta {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}

.term-chip {
  display: inline-flex;
  align-items: baseline;
  gap: var(--space-1);
  min-width: 0;
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.term-cwd {
  overflow: hidden;
}

.term-cwd .mono {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.term-key {
  color: var(--meta);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  font-size: var(--text-xs);
}

.term-session {
  color: var(--meta);
  font-size: var(--text-xs);
}

.term-shortcuts-btn {
  /* WCAG 2.5.8: the trigger keeps a 24x24 target (Naive's tiny 26x22 is
   * below the minimum). The visual icon stays small; the hit box grows. */
  min-width: var(--space-6);
  min-height: var(--space-6);
  margin-left: var(--space-1);
}

.term-commands-btn {
  margin-left: var(--space-1);
}

.term-shortcuts-note {
  max-width: 26rem;
}

.term-shortcuts {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  max-width: min(26rem, 80vw);
}

.term-shortcuts-title {
  margin: 0;
  font-weight: 600;
  color: var(--fg-2);
}

.term-shortcuts dl {
  display: grid;
  gap: var(--space-1);
  margin: 0;
}

.term-shortcuts dl > div {
  display: grid;
  grid-template-columns: minmax(0, 9rem) minmax(0, 1fr);
  gap: var(--space-2);
  align-items: baseline;
}

.term-shortcuts dt {
  margin: 0;
  color: var(--fg-2);
  font-size: var(--text-xs);
}

.term-shortcuts dd {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-xs);
}

/* Phone width (390px): the meta row wraps instead of clipping the working
 * directory to a single character. The key labels and the session id are
 * diagnostic only; the cwd takes its own full row, while the action buttons
 * stay on the first row. */
@media (max-width: 520px) {
  .term-meta {
    flex-wrap: wrap;
    row-gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
  }

  .term-key,
  .term-session {
    display: none;
  }

  .term-cwd {
    order: 10;
    flex: 1 1 100%;
  }

  .term-shortcuts-btn {
    margin-left: 0;
  }
}
</style>
