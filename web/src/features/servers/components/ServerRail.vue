<script setup lang="ts">
import { computed, onMounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import RailIconButton from "@/features/servers/components/RailIconButton.vue";
import NotificationBell from "@/features/notifications/components/NotificationBell.vue";
import {
  serverInitials,
  serverTip,
  statusDots,
} from "@/features/servers/utils/serverRailView";
import { useServersStore } from "@/features/servers/stores/servers";
import { useEditorStore } from "@/features/files/stores/editor";
import { useEditorWindowStore } from "@/features/files/stores/editorWindow";
import { useAuthStore } from "@/features/auth";
import { useTerminalStore, useTerminalWindowStore } from "@/features/terminal";
import { isContainerTerminalTarget } from "@/features/terminal/utils/terminal";

const serversStore = useServersStore();
const authStore = useAuthStore();
const leftEditor = useEditorStore("left");
const rightEditor = useEditorStore("right");
const editorWindow = useEditorWindowStore();
const terminal = useTerminalStore();
const terminalWindow = useTerminalWindowStore();
const route = useRoute();
const router = useRouter();
const { locale, t } = useI18n();

const isDashboard = computed<boolean>(() => route.name === "dashboard");

const editorCount = computed<number>(
  () => leftEditor.tabs.length + rightEditor.tabs.length,
);

const editorDirty = computed<boolean>(
  () => leftEditor.hasDirty || rightEditor.hasDirty,
);

const editorLabel = computed<string>(() =>
  t("servers.rail.editorWithCount", { count: editorCount.value }),
);

function toggleEditor(): void {
  editorWindow.setMinimized(!editorWindow.minimized);
}

const terminalTabs = computed(() =>
  terminal.tabs.filter((tab) =>
    isContainerTerminalTarget(tab.target) ? true : authStore.isOwner,
  ),
);

const terminalCount = computed<number>(() => terminalTabs.value.length);

const terminalWindowCount = computed<number>(
  () => terminalTabs.value.filter((tab) => tab.mode === "window").length,
);

const terminalLabel = computed<string>(() =>
  terminalWindowCount.value > 0
    ? t("terminal.rail.openWithCount", { count: terminalCount.value })
    : t("terminal.rail.pageWithCount", { count: terminalCount.value }),
);

function toggleTerminal(): void {
  if (terminalWindowCount.value === 0) {
    const pageTab = terminalTabs.value.find((tab) => tab.mode === "page");
    if (pageTab?.sourcePath) {
      void router.push(pageTab.sourcePath);
      return;
    }
    void router.push({ name: "terminal" });
    return;
  }
  if (terminalWindow.minimized) {
    terminalWindow.setMinimized(false);
    terminalWindow.raise();
    return;
  }
  if (!terminalWindow.raised) {
    terminalWindow.raise();
    return;
  }
  terminalWindow.setMinimized(true);
}

onMounted(() => {
  if (serversStore.servers.length === 0 && !serversStore.loading) {
    void serversStore.fetchServers().catch(() => {
    });
  }
});
</script>

<template>
  <nav class="rail" :aria-label="$t('servers.rail.navLabel')">
    <div class="rail-item" :class="{ 'is-active': isDashboard }">
      <RailIconButton :to="{ name: 'dashboard' }" :label="$t('servers.rail.home')">
        <span class="rail-brand" aria-hidden="true"></span>
      </RailIconButton>
    </div>

    <div class="rail-sep" aria-hidden="true"></div>

    <div class="rail-nav" role="list" :aria-label="$t('servers.rail.managedServers')">
      <div
        v-for="server in serversStore.servers"
        :key="server.id"
        class="rail-item"
        role="listitem"
      >
        <RailIconButton
          :to="{ name: 'server-detail', params: { id: server.id } }"
          :label="serverTip(server, locale)"
        >
          {{ serverInitials(server.name) }}
          <span
            class="rail-dot"
            :class="statusDots[server.status]"
            aria-hidden="true"
          ></span>
        </RailIconButton>
      </div>
    </div>

    <div class="rail-item">
      <RailIconButton
        :to="{ name: 'servers', query: { add: '1' } }"
        :label="$t('servers.rail.addServer')"
        link-class="rail-btn--add"
      >
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.6"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M12 5v14M5 12h14" />
        </svg>
      </RailIconButton>
    </div>

    <div class="rail-foot">
      <div class="rail-sep" aria-hidden="true"></div>
      <div v-if="editorCount > 0" class="rail-item">
        <button
          type="button"
          class="rail-btn rail-btn--editor"
          :aria-label="editorLabel"
          @click="toggleEditor"
        >
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="m16 18 6-6-6-6" />
            <path d="m8 6-6 6 6 6" />
          </svg>
          <span class="rail-badge" aria-hidden="true">{{
            editorCount > 9 ? "9+" : editorCount
          }}</span>
          <span v-if="editorDirty" class="rail-dirty" aria-hidden="true"></span>
        </button>
      </div>
      <div v-if="terminalCount > 0" class="rail-item">
        <button
          type="button"
          class="rail-btn rail-btn--terminal"
          :aria-label="terminalLabel"
          @click="toggleTerminal"
        >
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="m5 8 4 4-4 4" />
            <path d="M11 16h8" />
          </svg>
          <span class="rail-badge" aria-hidden="true">{{
            terminalCount > 9 ? "9+" : terminalCount
          }}</span>
        </button>
      </div>
      <div
        v-if="editorCount > 0 || terminalCount > 0"
        class="rail-sep"
        aria-hidden="true"
      ></div>
      <div class="rail-item">
        <RailIconButton :to="{ name: 'dashboard' }" :label="$t('servers.rail.overview')">
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="M4 7h10M18 7h2M4 17h4M12 17h8" />
            <circle cx="16" cy="7" r="2" />
            <circle cx="10" cy="17" r="2" />
          </svg>
        </RailIconButton>
      </div>
      <div class="rail-item">
        <NotificationBell variant="rail" />
      </div>
    </div>
  </nav>
</template>

<style scoped>
.rail {
  background: var(--surface-warm);
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: var(--space-3) 0;
  gap: var(--space-2);
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
}

.rail-nav {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
}

.rail-sep {
  width: 32px;
  height: 2px;
  border-radius: var(--radius-pill);
  background: var(--border-soft);
  margin: var(--space-1) 0;
  flex: 0 0 auto;
}

.rail-item {
  position: relative;
  display: grid;
  place-items: center;
  width: 100%;
}

:deep(.rail-btn) {
  position: relative;
  width: 48px;
  height: 48px;
  display: grid;
  place-items: center;
  background: var(--bg);
  color: var(--fg-2);
  font-family: var(--font-display);
  font-size: var(--text-lg);
  font-weight: 700;
  border-radius: var(--radius-lg);
  transition:
    border-radius 350ms var(--ease-morph),
    background var(--motion-base) var(--ease-standard),
    color var(--motion-base) var(--ease-standard);
}

:deep(.rail-btn:hover),
:deep(.rail-btn:focus-visible) {
  border-radius: var(--radius-pill);
  background: var(--accent);
  color: var(--accent-on);
}

:deep(.rail-btn) svg {
  width: 22px;
  height: 22px;
}

.rail-brand {
  display: block;
  height: var(--brand-mark-h);
  aspect-ratio: 942 / 534;
  background-color: currentColor;
  -webkit-mask: url("/logo/gotham-icon.svg") center / contain no-repeat;
  mask: url("/logo/gotham-icon.svg") center / contain no-repeat;
}

:deep(.rail-btn--add) {
  background: var(--surface);
  color: var(--success);
}

.rail-btn--editor,
.rail-btn--terminal {
  border: 0;
  padding: 0;
  font: inherit;
  cursor: pointer;
}

.rail-btn--editor {
  position: relative;
}

.rail-btn--editor .rail-badge,
.rail-btn--terminal .rail-badge {
  top: calc(-1 * var(--space-1));
  right: calc(-1 * var(--space-1));
  width: var(--space-6);
  height: var(--space-6);
  padding: 0;
  box-sizing: border-box;
  border-radius: 50%;
}

.rail-btn--editor .rail-dirty {
  position: absolute;
  left: 4px;
  bottom: 4px;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--warn);
}

.rail-item.is-active :deep(.rail-btn) {
  border-radius: var(--radius-lg);
  background: var(--accent);
  color: var(--accent-on);
}

.rail-item.is-active :deep(.rail-btn:hover) {
  border-radius: var(--radius-pill);
}

.rail-item::before {
  content: "";
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 4px;
  height: 0;
  background: var(--fg-2);
  border-radius: 0 var(--radius-pill) var(--radius-pill) 0;
  transition: height var(--motion-base) var(--ease-standard);
}

.rail-item:hover::before {
  height: 20px;
}

.rail-item.is-active::before {
  height: 40px;
}

.rail-dot {
  position: absolute;
  right: 3px;
  bottom: 3px;
  width: 10px;
  height: 10px;
  border-radius: var(--radius-pill);
  border: 3px solid var(--surface-warm);
  box-sizing: content-box;
}

.dot--online {
  background: var(--success);
}

.dot--idle {
  background: var(--warn);
}

.dot--dnd {
  background: var(--danger);
}

.dot--offline {
  background: var(--danger);
}

:deep(.rail-badge) {
  position: absolute;
  right: 6px;
  top: 4px;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  display: grid;
  place-items: center;
  background: var(--danger);
  color: var(--accent-on);
  border: 3px solid var(--surface-warm);
  box-sizing: content-box;
  border-radius: var(--radius-pill);
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 700;
  line-height: 1;
}

.rail-foot {
  margin-top: auto;
  position: sticky;
  bottom: 0;
  z-index: 2;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding-top: var(--space-2);
  background: var(--surface-warm);
}
</style>
