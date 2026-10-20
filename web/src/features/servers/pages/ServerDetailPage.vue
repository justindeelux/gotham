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
import { useServerDetailTab } from "@/features/servers/composables/useServerDetailTab";
import { useAuthStore } from "@/features/auth";
import { useServersStore } from "@/features/servers/stores/servers";
import ServerTerminalTab from "@/features/servers/components/ServerTerminalTab.vue";

const route = useRoute();
const router = useRouter();
const message = useMessage();
const { locale, t } = useI18n();
const serversStore = useServersStore();
const authStore = useAuthStore();

const serverId = computed<string>(() => String(route.params.id ?? ""));

const { activeTab, blockedTerminal, showOverview } = useServerDetailTab(serverId);

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
  resetForServer();
  void fetchServer();
});

// Demotion is enforced by the backend and must leave the operator tab even
// though its pane is removed by `v-if` at the same moment. A sign-out is not
// a demotion: the auth flow owns that redirect.
watch(
  [() => authStore.isAuthenticated, () => authStore.isPlatformAdmin],
  ([authenticated, admin]) => {
    if (authenticated && !admin) {
      showOverview();
    }
  },
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

        <NAlert
          v-if="blockedTerminal"
          type="info"
          :show-icon="true"
          class="server-blocked-tab"
        >
          <template #header>{{ $t("terminal.page.notAvailableTitle") }}</template>
          {{ $t("terminal.page.notAvailableBody") }}
        </NAlert>

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
            display-directive="show"
          >
            <ServerTerminalTab
              class="server-terminal"
              :server="liveServer"
              :server-id="serverId"
              :ready="liveServer?.status === 'ready'"
              :active="activeTab === 'terminal'"
              @blocked="showOverview"
            />
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
  /* The extracted stage owns the state view; keep a usable terminal height
   * on the detail page without letting the page grow unbounded. */
  display: flex;
  flex-direction: column;
  height: clamp(18rem, 60vh, 44rem);
  min-height: 0;
  margin-top: var(--space-4);
}

.server-blocked-tab {
  margin-bottom: var(--space-3);
}
</style>
