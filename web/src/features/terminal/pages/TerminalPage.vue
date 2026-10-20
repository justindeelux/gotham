<script setup lang="ts">
import { X } from "@lucide/vue";
import { NAlert } from "naive-ui";
import { computed, nextTick, watch } from "vue";
import { useI18n } from "vue-i18n";

import TerminalPane from "@/features/terminal/components/TerminalPane.vue";
import TerminalServerPicker from "@/features/terminal/components/TerminalServerPicker.vue";
import TerminalSessionStage from "@/features/terminal/components/TerminalSessionStage.vue";
import TerminalTargetPicker from "@/features/terminal/components/TerminalTargetPicker.vue";
import { useTerminalPageSession } from "@/features/terminal/composables/useTerminalPageSession";
import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalTab } from "@/features/terminal/stores/terminal";
import { isContainerTerminalTarget } from "@/features/terminal/utils/terminal";

/**
 * Standalone page terminal (`/terminal`, JUS-246).
 *
 * The page owns a tab strip now (JUS-265): the add-tab picker (+) after the
 * strip opens host or container sessions in new page tabs, each keeping its
 * own target label, state and close confirmation. The empty state keeps the
 * server cards for the first session; everything else is the shared session
 * stage. The page never creates a shell on mount or on a route change.
 */
const { t } = useI18n();
const terminal = useTerminalStore();
const {
  servers,
  pageTabs,
  selectedId,
  selectedServer,
  serverReady,
  selectedStatusLabel,
  tabId,
  activeTab,
  starting,
  closed,
  closedIsContainer,
  detachedTab,
  unknownServerId,
  startSession,
  restartClosed,
  restartDetached,
  selectServer,
  activateTab,
} = useTerminalPageSession();

const isContainer = computed<boolean>(() =>
  activeTab.value ? isContainerTerminalTarget(activeTab.value.target) : false,
);

/** stageServer is null for a container tab (no node lifecycle branch). */
const stageServer = computed(() =>
  isContainer.value ? null : selectedServer.value,
);

/** pageView is the single state the page renders below the tab strip. */
const pageView = computed<"session" | "detached" | "closed" | "picker">(() => {
  if (activeTab.value !== null) {
    return "session";
  }
  if (detachedTab.value !== null) {
    return "detached";
  }
  if (closed.value) {
    return "closed";
  }
  return "picker";
});

/** showNotReady explains an empty-state pick on an unavailable node. */
const showNotReady = computed<boolean>(
  () =>
    pageView.value === "picker" &&
    unknownServerId.value === "" &&
    selectedId.value !== "" &&
    !serverReady.value,
);

// A one-way transfer unmounts the pane that owned the trigger, so focus would
// fall to document.body. Hand it to the new window tab instead.
watch(detachedTab, (next, previous) => {
  if (next === null || previous !== null) {
    return;
  }
  void nextTick(() => {
    document.getElementById(`term-tab-${next.id}`)?.focus();
  });
});

/** requestCloseTab uses the shared confirmation path. */
function requestCloseTab(id: string): void {
  void terminal.requestClose([id]);
}

/** focusTab moves focus to one page tab (roving tablist). */
function focusTab(index: number): void {
  const tab = pageTabs.value[index];
  if (tab === undefined) {
    return;
  }
  activateTab(tab.id);
  void nextTick(() => {
    document.getElementById(`term-page-tab-${tab.id}`)?.focus();
  });
}

/** onTabKeydown implements roving focus and the Delete close path. */
function onTabKeydown(event: KeyboardEvent, index: number): void {
  const count = pageTabs.value.length;
  if (count === 0) {
    return;
  }
  if (event.key === "ArrowRight") {
    event.preventDefault();
    focusTab((index + 1) % count);
  } else if (event.key === "ArrowLeft") {
    event.preventDefault();
    focusTab((index - 1 + count) % count);
  } else if (event.key === "Home") {
    event.preventDefault();
    focusTab(0);
  } else if (event.key === "End") {
    event.preventDefault();
    focusTab(count - 1);
  } else if (event.key === "Delete") {
    event.preventDefault();
    const tab = pageTabs.value[index] as TerminalTab;
    requestCloseTab(tab.id);
  }
}
</script>

<template>
  <div class="term-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("terminal.page.eyebrow") }}</p>
        <h1>{{ t("terminal.page.title") }}</h1>
        <p class="page-desc">{{ t("terminal.page.description") }}</p>
      </div>
    </div>

    <div class="term-page-bar">
      <div
        v-if="pageTabs.length > 0"
        class="term-page-tabs"
        role="tablist"
        :aria-label="t('terminal.page.tabs')"
      >
        <div
          v-for="(tab, index) in pageTabs"
          :key="tab.id"
          class="term-page-tab"
          :class="{ 'is-active': tab.id === activeTab?.id }"
          role="presentation"
        >
          <button
            :id="`term-page-tab-${tab.id}`"
            type="button"
            class="term-page-tab-name"
            role="tab"
            :aria-selected="tab.id === activeTab?.id"
            :aria-controls="`term-page-panel-${tab.id}`"
            :tabindex="tab.id === activeTab?.id ? 0 : -1"
            :title="tab.title"
            aria-keyshortcuts="Delete"
            @click="activateTab(tab.id)"
            @keydown="onTabKeydown($event, index)"
          >
            <span
              class="term-page-tab-status"
              :class="`is-${tab.status}`"
              aria-hidden="true"
            />
            <span class="term-page-tab-label">{{ tab.title }}</span>
            <span
              class="term-page-tab-close"
              aria-hidden="true"
              :title="t('terminal.page.closeTab', { title: tab.title })"
              @click.stop="requestCloseTab(tab.id)"
            >
              <X :size="12" :stroke-width="2" aria-hidden="true" />
            </span>
          </button>
        </div>
      </div>
      <TerminalTargetPicker surface="page" />
    </div>

    <NAlert
      v-if="unknownServerId !== ''"
      type="error"
      :show-icon="true"
      class="term-page-alert"
    >
      {{ t("terminal.page.unknownServer") }}
    </NAlert>
    <NAlert
      v-else-if="showNotReady"
      type="warning"
      :show-icon="true"
      class="term-page-alert"
    >
      <template #header>{{ t("terminal.page.notReadyTitle") }}</template>
      {{
        t("terminal.page.notReadyBody", {
          status: selectedStatusLabel,
        })
      }}
    </NAlert>

    <TerminalServerPicker
      v-if="pageView === 'picker'"
      :servers="servers"
      @select="(serverId: string) => void selectServer(serverId)"
    />
    <TerminalPane
      v-else-if="pageView === 'detached' && detachedTab"
      :tab-id="detachedTab.id"
      @start-new="restartDetached"
    />
    <TerminalSessionStage
      v-else-if="pageView === 'closed'"
      :server="selectedServer"
      :ready="serverReady"
      tab-id=""
      :starting="starting"
      :closed="true"
      :mode="closedIsContainer ? 'application' : 'host'"
      @start="restartClosed"
    />
    <TerminalSessionStage
      v-else-if="activeTab"
      :key="activeTab.id"
      :id="`term-page-panel-${activeTab.id}`"
      :server="stageServer"
      :ready="isContainer || serverReady"
      :tab-id="tabId"
      :starting="false"
      :closed="false"
      :mode="isContainer ? 'application' : 'host'"
      @start="startSession"
    />
  </div>
</template>

<style scoped>
.term-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  height: 100%;
  min-height: 0;
}

.page-head {
  flex: 0 0 auto;
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin: 0 0 var(--space-2);
}

.page-head h1 {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.term-page-alert {
  flex: 0 0 auto;
}

.term-page-bar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-1);
  min-width: 0;
}

.term-page-tabs {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  min-width: 0;
  max-width: min(60%, 48rem);
  overflow-x: auto;
  scrollbar-width: none;
}

.term-page-tabs::-webkit-scrollbar {
  display: none;
}

.term-page-tab {
  display: inline-flex;
  align-items: center;
  min-width: 0;
  max-width: min(16rem, 45vw);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-warm);
  color: var(--muted);
}

.term-page-tab.is-active {
  background: var(--bg);
  color: var(--fg-2);
  border-color: var(--accent);
}

.term-page-tab-name {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  min-width: 0;
  max-width: min(14rem, 40vw);
  padding: var(--space-1) var(--space-2);
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  font-size: var(--text-xs);
  cursor: pointer;
  white-space: nowrap;
}

.term-page-tab-name:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
  border-radius: var(--radius-sm);
}

.term-page-tab-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.term-page-tab-status {
  width: var(--space-1);
  height: var(--space-1);
  border-radius: var(--radius-pill);
  flex: 0 0 auto;
  background: var(--meta);
}

.term-page-tab-status.is-connected {
  background: var(--success);
}

.term-page-tab-status.is-connecting {
  background: var(--accent);
}

.term-page-tab-status.is-exited,
.term-page-tab-status.is-disconnected {
  background: var(--warn);
}

.term-page-tab-status.is-error {
  background: var(--danger);
}

.term-page-tab-close {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: var(--space-6);
  height: var(--space-6);
  margin-right: calc(-1 * var(--space-1));
  border-radius: var(--radius-sm);
  color: var(--meta);
  cursor: pointer;
}

.term-page-tab-close:hover {
  background: var(--border);
  color: var(--fg-2);
}

@media (max-width: 640px) {
  .term-page-tabs {
    max-width: calc(100% - var(--space-8));
  }

  .term-page-tab {
    max-width: min(10rem, 52vw);
  }

  .term-page-tab-name {
    max-width: min(8rem, 46vw);
  }
}
</style>
