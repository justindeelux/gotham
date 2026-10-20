<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NSelect, NSpin } from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import type { Server } from "@/features/servers";
import { useServersStore } from "@/features/servers";
import { TerminalPane, useTerminalStore } from "@/features/terminal";

/**
 * Standalone page terminal (`/terminal`, JUS-246).
 *
 * The operator picks an authorized server first; only then is a page-mode
 * session requested. `openPageSession` is idempotent and single-flight, so
 * revisiting the route (or the app dock's resume action) reuses the live
 * session instead of starting a second shell. Switching servers runs the
 * shared confirmation before the old page shell is released; a detached
 * session already lives in the window and is left untouched.
 */
const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const serversStore = useServersStore();
const terminal = useTerminalStore();

const selectedId = ref<string>("");
const tabId = ref<string>("");
const loading = ref(false);
/** syncingQuery guards the route → selection echo while we write the URL. */
let syncingQuery = false;

const servers = computed<Server[]>(() => serversStore.servers);
const selectedServer = computed<Server | null>(
  () => servers.value.find((server) => server.id === selectedId.value) ?? null,
);
const serverReady = computed<boolean>(
  () => selectedServer.value?.status === "ready",
);

/** serverOptions label each node with its live status for the switcher. */
const serverOptions = computed(() =>
  servers.value.map((server) => ({
    label:
      server.status === "ready"
        ? server.name
        : `${server.name} · ${t(`servers.status.${server.status}`)}`,
    value: server.id,
  })),
);

/** sourcePath is the route the app dock resumes for this page session. */
function sourcePathFor(serverId: string): string {
  return router.resolve({ name: "terminal", query: { server: serverId } }).fullPath;
}

/** statusLabel localizes the server lifecycle word. */
function statusLabel(server: Server): string {
  return t(`servers.status.${server.status}`);
}

/** syncQuery mirrors the selection into the URL for reload/dock resume. */
function syncQuery(): void {
  const current = typeof route.query.server === "string" ? route.query.server : "";
  if (current === selectedId.value) {
    return;
  }
  syncingQuery = true;
  const query = { ...route.query };
  if (selectedId.value === "") {
    delete query.server;
  } else {
    query.server = selectedId.value;
  }
  void router.replace({ name: "terminal", query }).finally(() => {
    syncingQuery = false;
  });
}

/** openSelected starts or reuses the page session for the current server. */
async function openSelected(): Promise<void> {
  const server = selectedServer.value;
  if (!server || server.id === "" || server.status !== "ready") {
    tabId.value = "";
    return;
  }
  const request = {
    serverId: server.id,
    serverName: server.name,
    sourcePath: sourcePathFor(server.id),
  };
  const existing = terminal.findTabForTarget(request);
  if (existing) {
    tabId.value = existing.id;
    return;
  }
  loading.value = true;
  try {
    const tab = await terminal.openPageSession(request);
    if (selectedId.value === server.id) {
      tabId.value = tab.id;
    }
  } finally {
    loading.value = false;
  }
}

/**
 * selectServer runs the confirmed close lifecycle when the old page shell
 * would otherwise be stranded: cancel keeps it and restores the switcher,
 * confirm releases exactly that session before the next one starts.
 */
async function selectServer(nextId: string): Promise<void> {
  if (nextId === "" || nextId === selectedId.value) {
    return;
  }
  const previous = selectedId.value;
  const previousTab = previous
    ? terminal.pageTabs.find((tab) => tab.serverId === previous)
    : undefined;
  if (previousTab && previousTab.mode === "page") {
    const confirmed = await terminal.requestClose([previousTab.id]);
    if (!confirmed) {
      selectedId.value = previous;
      return;
    }
  }
  // A newer selection landed while the dialog was open.
  if (selectedId.value !== previous) {
    return;
  }
  selectedId.value = nextId;
  tabId.value = "";
  syncQuery();
  await openSelected();
}

/** onServerSelect adapts Naive's value type to the selection flow. */
function onServerSelect(value: string | number | null): void {
  void selectServer(String(value ?? ""));
}

// The URL is the resume contract (dock, bookmarks, back/forward).
watch(
  () => route.query.server,
  (value) => {
    if (syncingQuery) {
      return;
    }
    const next = typeof value === "string" ? value : "";
    if (next === selectedId.value) {
      return;
    }
    if (next === "") {
      selectedId.value = "";
      tabId.value = "";
      return;
    }
    void selectServer(next);
  },
);

// A node that comes online while its server is selected starts the shell the
// operator already asked for; nothing is created before that.
watch(
  () => selectedServer.value?.status,
  (status) => {
    if (status === "ready" && selectedId.value !== "" && tabId.value === "") {
      void openSelected();
    }
  },
);

onMounted(async () => {
  if (servers.value.length === 0 && !serversStore.loading) {
    await serversStore.fetchServers().catch(() => undefined);
  }
  const queryId =
    typeof route.query.server === "string" ? route.query.server : "";
  if (queryId !== "") {
    selectedId.value = queryId;
    await openSelected();
    return;
  }
  // Re-entering the menu with exactly one live page session resumes it; the
  // store reuses the shell, so no second create is issued.
  const pageTabs = terminal.pageTabs;
  if (pageTabs.length === 1) {
    selectedId.value = pageTabs[0]!.serverId;
    syncQuery();
    await openSelected();
  }
});
</script>

<template>
  <div class="term-page">
    <div class="page-head">
      <p class="eyebrow">{{ t("terminal.page.eyebrow") }}</p>
      <h1>{{ t("terminal.page.title") }}</h1>
      <p class="page-desc">{{ t("terminal.page.description") }}</p>
    </div>

    <div v-if="selectedId !== ''" class="term-toolbar">
      <label class="term-toolbar-label" for="term-server-select">
        {{ t("terminal.page.switchServer") }}
      </label>
      <NSelect
        id="term-server-select"
        class="term-server-select"
        size="small"
        :value="selectedId"
        :options="serverOptions"
        :aria-label="t('terminal.page.switchServer')"
        @update:value="onServerSelect"
      />
      <span
        v-if="selectedServer"
        class="term-toolbar-status"
        :class="`is-${selectedServer.status}`"
      >
        {{ statusLabel(selectedServer) }}
      </span>
    </div>

    <div v-if="selectedId === ''" class="term-chooser">
      <NCard>
        <div v-if="servers.length > 0" class="server-picker">
          <button
            v-for="server in servers"
            :key="server.id"
            type="button"
            class="server-pick"
            :class="{ 'is-unavailable': server.status !== 'ready' }"
            @click="selectServer(server.id)"
          >
            <span class="term-pick-head">
              <span class="term-pick-name">{{ server.name }}</span>
              <span class="term-pick-status" :class="`is-${server.status}`">
                {{ statusLabel(server) }}
              </span>
            </span>
            <span class="term-pick-meta mono">{{ server.ip }}</span>
            <span class="term-pick-go">{{ t("terminal.page.chooseServer") }}</span>
          </button>
        </div>
        <NEmpty v-else :description="t('terminal.page.empty')">
          <template #extra>
            <p class="term-empty-hint">{{ t("terminal.page.emptyHint") }}</p>
            <RouterLink :to="{ name: 'servers' }" custom>
              <template #default="{ navigate }">
                <NButton type="primary" @click="navigate">
                  {{ t("terminal.page.addServer") }}
                </NButton>
              </template>
            </RouterLink>
          </template>
        </NEmpty>
      </NCard>
    </div>

    <div v-else class="term-stage">
      <NAlert
        v-if="selectedServer === null"
        type="error"
        :show-icon="true"
        class="term-stage-alert"
      >
        {{ t("terminal.page.unknownServer") }}
      </NAlert>
      <NAlert
        v-else-if="!serverReady"
        type="warning"
        :show-icon="true"
        class="term-stage-alert"
      >
        <template #header>{{ t("terminal.page.notReadyTitle") }}</template>
        {{
          t("terminal.page.notReadyBody", {
            status: statusLabel(selectedServer),
          })
        }}
      </NAlert>
      <div v-else-if="tabId === ''" class="term-stage-loading">
        <NSpin size="small" :aria-label="t('terminal.page.starting')" />
        <p class="small muted">{{ t("terminal.page.starting") }}</p>
      </div>
      <TerminalPane v-else :tab-id="tabId" class="term-stage-pane" />
    </div>
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
  font-size: 11px;
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

.term-toolbar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.term-toolbar-label {
  font-size: var(--text-xs);
  color: var(--muted);
}

.term-server-select {
  max-width: 22rem;
}

.term-toolbar-status {
  font-size: var(--text-xs);
  color: var(--muted);
}

.term-toolbar-status.is-ready {
  color: var(--success);
}

.term-toolbar-status.is-error,
.term-toolbar-status.is-offline {
  color: var(--danger);
}

.term-toolbar-status.is-pending,
.term-toolbar-status.is-validating {
  color: var(--warn);
}

.term-chooser {
  flex: 0 0 auto;
}

.server-picker {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: var(--space-3);
}

.server-pick {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition:
    border-color var(--motion-base) var(--ease-standard),
    background var(--motion-base) var(--ease-standard);
}

.server-pick:hover {
  border-color: var(--accent);
  background: color-mix(in oklab, var(--surface) 88%, var(--accent));
}

.server-pick:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

.server-pick.is-unavailable {
  opacity: 0.72;
}

.term-pick-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}

.term-pick-name {
  font-weight: 600;
  color: var(--fg-2);
}

.term-pick-status {
  font-size: var(--text-xs);
  color: var(--muted);
}

.term-pick-status.is-ready {
  color: var(--success);
}

.term-pick-status.is-error,
.term-pick-status.is-offline {
  color: var(--danger);
}

.term-pick-status.is-pending,
.term-pick-status.is-validating {
  color: var(--warn);
}

.term-pick-meta {
  color: var(--muted);
  font-size: var(--text-xs);
}

.term-pick-go {
  margin-top: var(--space-2);
  color: var(--accent-ink);
  font-size: var(--text-xs);
}

.term-empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
}

.term-stage {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.term-stage-alert {
  flex: 0 0 auto;
}

.term-stage-loading {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
}

.term-stage-pane {
  flex: 1 1 auto;
  min-height: 0;
}

@media (max-width: 640px) {
  .term-toolbar {
    flex-wrap: wrap;
    row-gap: var(--space-1);
  }
}
</style>
