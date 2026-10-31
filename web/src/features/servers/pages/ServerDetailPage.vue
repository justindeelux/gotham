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
import ServerContainersTab from "@/features/servers/components/ServerContainersTab.vue";
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
import { useServerUsageRefresh } from "@/features/servers/composables/useServerUsageRefresh";
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
useServerUsageRefresh(activeTab);

const server = ref<Server | null>(null);
const loading = ref(false);
const fetchFailure = ref<unknown>(null);
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

const liveServer = computed<Server | null>(() => {
  const local = server.value;
  const cached = serversStore.servers.find((item) => item.id === serverId.value);
  return cached ?? local ?? null;
});

const { context: metricsContext, resetForServer } = useServerMetrics(serverId, activeTab);
provide(ServerMetricsKey, metricsContext);

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

function handleUpdated(updated: Server): void {
  server.value = updated;
  message.success(t("servers.toasts.saved", { name: updated.name }));
}

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

watch(
  [() => authStore.isAuthenticated, () => authStore.isOwner],
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
      <div v-if="error" class="page-banners">
        <NAlert type="error" :show-icon="true">
          {{ error }}
        </NAlert>
      </div>

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
              :server="liveServer ?? server"
              :deleting="deleting"
              @delete="handleDelete"
            />
          </NTabPane>

          <NTabPane
            name="containers"
            :tab="$t('servers.detail.tabContainers')"
            display-directive="if"
          >
            <ServerContainersTab :server-id="serverId" />
          </NTabPane>

          <NTabPane
            v-if="authStore.isOwner"
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
            v-if="authStore.isOwner"
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

          <NTabPane
            v-if="authStore.isOwner"
            name="settings"
            :tab="$t('servers.detail.tabSettings')"
          >
            <ServerSettingsTab :server="server" @server-changed="fetchServer" />
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
  height: clamp(18rem, 60vh, 44rem);
  min-height: 0;
  margin-top: var(--space-4);
}

.server-blocked-tab {
  margin-bottom: var(--space-3);
}
</style>
