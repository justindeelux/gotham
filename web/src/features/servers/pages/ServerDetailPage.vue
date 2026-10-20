<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
  useMessage,
} from "naive-ui";
import { computed, onMounted, provide, ref, watch } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import type { Server } from "@/features/servers/api/servers";
import { describeServerError, failureText, getServer } from "@/features/servers/api/servers";
import EditServerModal from "@/features/servers/components/EditServerModal.vue";
import ServerDetailHeader from "@/features/servers/components/ServerDetailHeader.vue";
import ServerMetricsTab from "@/features/servers/components/ServerMetricsTab.vue";
import ServerOverviewTab from "@/features/servers/components/ServerOverviewTab.vue";
import ServerProxyTab from "@/features/servers/components/ServerProxyTab.vue";
import ServerSettingsTab from "@/features/servers/components/ServerSettingsTab.vue";
import {
  ServerMetricsKey,
  useServerMetrics,
} from "@/features/servers/composables/useServerMetrics";
import { useAuthStore } from "@/features/auth";
import { useServersStore } from "@/features/servers/stores/servers";
import { TerminalPane, useTerminalStore } from "@/features/terminal";

const route = useRoute();
const router = useRouter();
const message = useMessage();
const { locale, t } = useI18n();
const serversStore = useServersStore();
const authStore = useAuthStore();
const terminal = useTerminalStore();

const serverId = computed<string>(() => String(route.params.id ?? ""));

/** DETAIL_TABS is the tab order shared with the URL `tab` query. */
const DETAIL_TABS = [
  "overview",
  "containers",
  "files",
  "terminal",
  "metrics",
  "proxy",
  "settings",
] as const;
type DetailTab = (typeof DETAIL_TABS)[number];

/** isDetailTab narrows one untrusted query value. */
function isDetailTab(value: unknown): value is DetailTab {
  return typeof value === "string" && (DETAIL_TABS as readonly string[]).includes(value);
}

/**
 * tabFromQuery restores the tab after a reload or a dock resume. The
 * operator-only tabs are ignored for a caller who cannot see them, so a
 * crafted URL never leaves the tab strip on a hidden pane.
 */
function tabFromQuery(): DetailTab {
  const value = route.query.tab;
  if (!isDetailTab(value)) {
    return "overview";
  }
  if ((value === "files" || value === "terminal") && !authStore.isPlatformAdmin) {
    return "overview";
  }
  return value;
}

const server = ref<Server | null>(null);
const loading = ref(false);
/**
 * fetchFailure keeps the raw last detail failure. The `error` display
 * derives from it in the current locale, so a language switch re-renders
 * a retained banner without reloading the server.
 */
const fetchFailure = ref<unknown>(null);
/** error renders the retained failure, or null while healthy. */
const error = computed<string | null>(() => {
  if (!serverId.value) {
    return t("servers.detail.unknownServer");
  }
  if (fetchFailure.value === null || fetchFailure.value === undefined) {
    return null;
  }
  return failureText(fetchFailure.value, locale.value);
});
const validating = ref(false);
const deleting = ref(false);
const editOpen = ref(false);
const activeTab = ref<DetailTab>(tabFromQuery());

/**
 * Terminal tab state (JUS-246). The tab is lazy: `activeTab === "terminal"`
 * is what requests the page session, so opening the server detail page or
 * switching other tabs issues zero create calls. The id is reset on a route
 * change so one server's pane can never render another server's session.
 */
const terminalTabId = ref<string>("");

/**
 * liveServer merges the polled list status into the detail row, so a node
 * that goes offline (or comes back) is reflected without a manual reload.
 */
const liveServer = computed<Server | null>(() => {
  const local = server.value;
  const cached = serversStore.servers.find((item) => item.id === serverId.value);
  if (!local) {
    return cached ?? null;
  }
  if (!cached || cached.status === local.status) {
    return local;
  }
  return { ...local, status: cached.status };
});

/** terminalStatus is the lifecycle word the unavailable panel shows. */
const terminalStatus = computed<string>(() =>
  liveServer.value ? t(`servers.status.${liveServer.value.status}`) : t("terminal.page.unknownServer"),
);

/** terminalSourcePath is the route the app dock resumes for this session. */
function terminalSourcePath(id: string): string {
  return router.resolve({
    name: "server-detail",
    params: { id },
    query: { tab: "terminal" },
  }).fullPath;
}

/**
 * ensureTerminalSession creates or reuses the page shell for this server.
 * `openPageSession` is single-flight and idempotent per target, so rapid tab
 * changes or a remount never open a concurrent duplicate.
 */
async function ensureTerminalSession(): Promise<void> {
  const current = liveServer.value;
  if (
    !current ||
    current.id !== serverId.value ||
    current.status !== "ready" ||
    !authStore.isPlatformAdmin
  ) {
    return;
  }
  const request = {
    serverId: current.id,
    serverName: current.name,
    sourcePath: terminalSourcePath(current.id),
  };
  const existing = terminal.findTabForTarget(request);
  if (existing) {
    terminalTabId.value = existing.id;
    return;
  }
  try {
    const tab = await terminal.openPageSession(request);
    if (serverId.value === current.id) {
      terminalTabId.value = tab.id;
    }
  } catch {
    // The pane renders its own retry/error state; the tab stays mountable.
  }
}

// One shared metrics window for the page and the metrics tab.
const { context: metricsContext, resetForServer } = useServerMetrics(serverId, activeTab);
provide(ServerMetricsKey, metricsContext);

/** fetchServer loads one server by route id; 404 surfaces as an error state. */
async function fetchServer(): Promise<void> {
  if (!serverId.value) {
    return;
  }
  loading.value = true;
  fetchFailure.value = null;
  try {
    server.value = await getServer(serverId.value);
  } catch (err) {
    server.value = null;
    fetchFailure.value = err;
  } finally {
    loading.value = false;
  }
}

/** handleValidate runs the SSH probes and refreshes the header. */
async function handleValidate(): Promise<void> {
  if (!server.value) {
    return;
  }
  validating.value = true;
  try {
    const outcome = await serversStore.validate(server.value.id);
    if (outcome.server) {
      server.value = outcome.server;
    }
    if (outcome.ok) {
      message.success(t("servers.toasts.validationPassed", { name: server.value.name }));
      return;
    }
    const failed = outcome.checks
      .filter((check) => !check.ok)
      .map((check) => check.name)
      .join(", ");
    message.error(
      outcome.message || t("servers.toasts.validationFailed", { name: server.value.name, failed }),
    );
  } catch (err) {
    message.error(describeServerError(err));
  } finally {
    validating.value = false;
  }
}

/** handleUpdated applies an edit-modal save to the header. */
function handleUpdated(updated: Server): void {
  server.value = updated;
  message.success(t("servers.toasts.saved", { name: updated.name }));
}

/** handleDelete removes the server and returns to the list. */
async function handleDelete(): Promise<void> {
  if (!server.value) {
    return;
  }
  const name = server.value.name;
  deleting.value = true;
  try {
    await serversStore.removeServer(server.value.id);
    message.success(t("servers.toasts.deleted", { name }));
    await router.push({ name: "servers" });
  } catch (err) {
    message.error(describeServerError(err));
  } finally {
    deleting.value = false;
  }
}

watch(serverId, () => {
  activeTab.value = tabFromQuery();
  terminalTabId.value = "";
  resetForServer();
  void fetchServer();
});

// The URL keeps the active tab so the dock can resume a paused page session
// (`?tab=terminal`) and a reload lands on the same pane.
let syncingTabQuery = false;
watch(activeTab, (next) => {
  const desired = next === "terminal" ? "terminal" : null;
  const current = typeof route.query.tab === "string" ? route.query.tab : null;
  if (current === desired) {
    return;
  }
  syncingTabQuery = true;
  const query = { ...route.query };
  if (desired === null) {
    delete query.tab;
  } else {
    query.tab = desired;
  }
  void router
    .replace({
      name: "server-detail",
      params: { id: serverId.value },
      query,
    })
    .finally(() => {
      syncingTabQuery = false;
    });
});

watch(
  () => route.query.tab,
  (value) => {
    if (syncingTabQuery) {
      return;
    }
    const next = isDetailTab(value) ? value : "overview";
    const visible =
      (next === "files" || next === "terminal") && !authStore.isPlatformAdmin
        ? "overview"
        : next;
    if (visible !== activeTab.value) {
      activeTab.value = visible;
    }
  },
);

// First activation of the Terminal tab starts the shell; a revisit reuses the
// live tab through the store's idempotent open. Nothing is created while
// another tab is active, and an offline/unready node is never dialled.
watch(
  [activeTab, () => liveServer.value?.status, () => liveServer.value?.id],
  () => {
    if (activeTab.value !== "terminal") {
      return;
    }
    void ensureTerminalSession();
  },
  { immediate: true },
);

onMounted(() => {
  void fetchServer();
});
</script>

<template>
  <NSpace vertical :size="16">
    <nav class="breadcrumb" :aria-label="$t('servers.detail.breadcrumbNav')">
      <RouterLink to="/servers">{{ $t("servers.detail.serversBreadcrumb") }}</RouterLink>
      <span class="breadcrumb__sep">/</span>
      <span class="muted">{{ server?.name ?? serverId }}</span>
    </nav>

    <NSpin :show="loading">
      <NAlert
        v-if="error"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ error }}
      </NAlert>

      <template v-if="server">
        <ServerDetailHeader
          :server="server"
          :validating="validating"
          @edit="editOpen = true"
          @validate="handleValidate"
        />

        <NTabs v-model:value="activeTab" type="line" animated>
          <NTabPane name="overview" :tab="$t('servers.detail.tabOverview')">
            <ServerOverviewTab
              :server="server"
              :deleting="deleting"
              @delete="handleDelete"
            />
          </NTabPane>

          <NTabPane name="containers" :tab="$t('servers.detail.tabContainers')">
            <NCard style="margin-top: 16px">
              <NEmpty
                :description="$t('servers.detail.containersEmpty')"
              >
                <template #extra>
                  <RouterLink
                    :to="{
                      name: 'server-containers',
                      params: { id: server.id },
                    }"
                    custom
                  >
                    <template #default="{ navigate }">
                      <NButton type="primary" @click="navigate">
                        {{ $t("servers.detail.openContainers") }}
                      </NButton>
                    </template>
                  </RouterLink>
                </template>
              </NEmpty>
            </NCard>
          </NTabPane>

          <NTabPane
            v-if="authStore.isPlatformAdmin"
            name="files"
            :tab="$t('servers.detail.tabFiles')"
          >
            <NCard style="margin-top: 16px">
              <NEmpty :description="$t('servers.detail.filesEmpty')">
                <template #extra>
                  <RouterLink
                    :to="{
                      name: 'server-files',
                      params: { id: server.id },
                    }"
                    custom
                  >
                    <template #default="{ navigate }">
                      <NButton type="primary" @click="navigate">
                        {{ $t("servers.detail.openFiles") }}
                      </NButton>
                    </template>
                  </RouterLink>
                </template>
              </NEmpty>
            </NCard>
          </NTabPane>

          <NTabPane
            v-if="authStore.isPlatformAdmin"
            name="terminal"
            :tab="$t('terminal.page.tabLabel')"
          >
            <div class="server-terminal">
              <NAlert
                v-if="!liveServer || liveServer.status !== 'ready'"
                type="warning"
                :show-icon="true"
              >
                <template #header>{{ $t("terminal.page.notReadyTitle") }}</template>
                {{
                  $t("terminal.page.notReadyBody", {
                    status: terminalStatus,
                  })
                }}
              </NAlert>
              <div v-else-if="terminalTabId === ''" class="server-terminal-loading">
                <NSpin size="small" :aria-label="$t('terminal.page.starting')" />
              </div>
              <TerminalPane
                v-else
                :tab-id="terminalTabId"
                class="server-terminal-pane"
              />
            </div>
          </NTabPane>

          <NTabPane name="metrics" :tab="$t('servers.detail.tabMetrics')">
            <ServerMetricsTab />
          </NTabPane>

          <NTabPane name="proxy" :tab="$t('servers.detail.tabProxy')">
            <ServerProxyTab :server-id="server.id" />
          </NTabPane>

          <NTabPane name="settings" :tab="$t('servers.detail.tabSettings')">
            <ServerSettingsTab :server="server" />
          </NTabPane>
        </NTabs>
      </template>
    </NSpin>

    <EditServerModal
      v-model:show="editOpen"
      :server="server"
      @updated="handleUpdated"
    />
  </NSpace>
</template>

<style scoped>
.breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
}

.breadcrumb__sep {
  color: var(--meta);
}

.muted {
  color: var(--muted);
}

.server-terminal {
  display: flex;
  flex-direction: column;
  min-height: 0;
  margin-top: var(--space-4);
}

.server-terminal-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  height: clamp(12rem, 30vh, 24rem);
}

/* The pane owns xterm's scroll area; keep a usable terminal height at every
 * viewport without letting the page grow unbounded. */
.server-terminal-pane {
  height: clamp(18rem, 60vh, 44rem);
  min-height: 0;
}
</style>
