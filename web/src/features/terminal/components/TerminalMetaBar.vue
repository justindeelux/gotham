<script setup lang="ts">
import { BookMarked, ExternalLink, Keyboard, X } from "@lucide/vue";
import { NButton, NPopover, NTooltip } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import QuickCommandsModal from "@/features/terminal/components/QuickCommandsModal.vue";
import TerminalThemeMenu from "@/features/terminal/components/TerminalThemeMenu.vue";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalTab } from "@/features/terminal/stores/terminal";
import { isContainerTerminalTarget } from "@/features/terminal/utils/terminal";

interface Props {
  tab: TerminalTab;
  variant: "page" | "window";
}

const props = defineProps<Props>();
const emit = defineEmits<{ inserted: [label: string]; "focus-terminal": [] }>();
const { t } = useI18n();
const terminal = useTerminalStore();

const shortcutsOpen = ref(false);
const commandsOpen = ref(false);
const detailsOpen = ref(false);

const isContainer = computed<boolean>(() =>
  isContainerTerminalTarget(props.tab.target),
);

const targetText = computed<string>(() =>
  isContainer.value
    ? props.tab.targetLabel || props.tab.containerName || props.tab.target
    : props.tab.target,
);

function detachToWindow(): void {
  terminal.detachToWindow(props.tab.id);
}

function closeTerminal(): void {
  terminal.requestClose([props.tab.id]);
}

function onInserted(label: string): void {
  emit("inserted", label);
}

function onCommandsClosed(): void {
  emit("focus-terminal");
}
</script>

<template>
  <div class="term-meta">
    <NPopover
      v-model:show="detailsOpen"
      trigger="click"
      placement="bottom-start"
    >
      <template #trigger>
        <button
          type="button"
          class="term-chip term-context"
          :title="t('terminal.pane.details')"
          :aria-expanded="detailsOpen"
        >
          <span class="term-key">
            {{
              isContainer
                ? t("terminal.pane.application")
                : t("terminal.pane.target")
            }}
          </span>
          <span class="mono term-context-value">{{ targetText }}</span>
        </button>
      </template>
      <dl class="term-details">
        <div>
          <dt class="term-key">{{ t("terminal.pane.target") }}</dt>
          <dd class="mono">{{ props.tab.target }}</dd>
        </div>
        <div v-if="isContainer && props.tab.applicationName">
          <dt class="term-key">{{ t("terminal.pane.application") }}</dt>
          <dd class="mono">{{ props.tab.applicationName }}</dd>
        </div>
        <div v-if="props.tab.cwd">
          <dt class="term-key">{{ t("terminal.pane.cwd") }}</dt>
          <dd class="mono term-details-cwd">{{ props.tab.cwd }}</dd>
        </div>
        <div v-if="props.tab.sessionId">
          <dt class="term-key">{{ t("terminal.pane.session") }}</dt>
          <dd class="mono">{{ props.tab.sessionId }}</dd>
        </div>
      </dl>
    </NPopover>
    <span v-if="props.tab.cwd" class="term-chip term-cwd" :title="props.tab.cwd">
      <span class="term-key">{{ t("terminal.pane.cwd") }}</span>
      <span class="mono">{{ props.tab.cwd }}</span>
    </span>
    <span class="grow" />
    <div class="term-meta-actions">
      <NPopover
        v-model:show="shortcutsOpen"
        trigger="click"
        placement="bottom-end"
      >
        <template #trigger>
          <NTooltip trigger="hover" placement="bottom">
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
            {{ t("terminal.shortcuts.show") }}
          </NTooltip>
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
      <NTooltip trigger="hover" placement="bottom">
        <template #trigger>
          <NButton
            quaternary
            size="tiny"
            class="term-commands-btn"
            :aria-label="t('terminal.quickCommands.open')"
            aria-haspopup="dialog"
            :aria-expanded="commandsOpen"
            @click.stop="commandsOpen = true"
          >
            <template #icon>
              <BookMarked :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
          </NButton>
        </template>
        {{ t("terminal.quickCommands.open") }}
      </NTooltip>
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
    <QuickCommandsModal
      v-model:show="commandsOpen"
      :tab-id="props.tab.id"
      @inserted="onInserted"
      @closed="onCommandsClosed"
    />
  </div>
</template>

<style scoped>
.term-meta {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  padding: var(--space-1) var(--space-3);
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

.term-context {
  flex: 0 1 auto;
  padding: var(--space-1) var(--space-2);
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  background: transparent;
  font: inherit;
  cursor: pointer;
  text-align: left;
}

.term-context:hover {
  border-color: var(--border-soft);
}

.term-context:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}

.term-context-value {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.term-cwd {
  flex: 0 1 auto;
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

.term-meta .grow {
  flex: 1 1 auto;
  min-width: 0;
}

.term-meta-actions {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: var(--space-1);
}

.term-details {
  display: grid;
  gap: var(--space-2);
  margin: 0;
  max-width: min(28rem, 80vw);
  font-size: var(--text-xs);
}

.term-details > div {
  display: grid;
  gap: 2px;
}

.term-details dd {
  margin: 0;
  color: var(--fg-2);
  overflow-wrap: anywhere;
}

.term-details-cwd {
  max-height: 8rem;
  overflow-y: auto;
}

.term-shortcuts-btn {
  min-width: var(--space-6);
  min-height: var(--space-6);
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

@media (max-width: 520px) {
  .term-meta {
    flex-wrap: wrap;
    row-gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
  }

  .term-key {
    display: none;
  }

  .term-cwd {
    order: 10;
    flex: 1 1 100%;
  }
}
</style>
