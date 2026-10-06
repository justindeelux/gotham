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
import { describeServerError, getServer } from "@/features/servers/api/servers";
import EditServerModal from "@/features/servers/components/EditServerModal.vue";
import ServerDetailHeader from "@/features/servers/components/ServerDetailHeader.vue";
import ServerMetricsTab from "@/features/servers/components/ServerMetricsTab.vue";
import ServerOverviewTab from "@/features/servers/components/ServerOverviewTab.vue";
import ServerSettingsTab from "@/features/servers/components/ServerSettingsTab.vue";
import {
  ServerMetricsKey,
  useServerMetrics,
} from "@/features/servers/composables/useServerMetrics";
import { useServersStore } from "@/features/servers/stores/servers";

const route = useRoute();
const router = useRouter();
const message = useMessage();
const { t } = useI18n();
const serversStore = useServersStore();

const serverId = computed<string>(() => String(route.params.id ?? ""));

const server = ref<Server | null>(null);
const loading = ref(false);
const error = ref<string | null>(null);
const validating = ref(false);
const deleting = ref(false);
const editOpen = ref(false);
const activeTab = ref("overview");

// One shared metrics window for the page and the metrics tab.
const { context: metricsContext, resetForServer } = useServerMetrics(serverId, activeTab);
provide(ServerMetricsKey, metricsContext);

/** fetchServer loads one server by route id; 404 surfaces as an error state. */
async function fetchServer(): Promise<void> {
  if (!serverId.value) {
    error.value = t("servers.detail.unknownServer");
    return;
  }
  loading.value = true;
  error.value = null;
  try {
    server.value = await getServer(serverId.value);
  } catch (err) {
    server.value = null;
    error.value = describeServerError(err);
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
  activeTab.value = "overview";
  resetForServer();
  void fetchServer();
});

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

          <NTabPane name="metrics" :tab="$t('servers.detail.tabMetrics')">
            <ServerMetricsTab />
          </NTabPane>

          <NTabPane name="proxy" :tab="$t('servers.detail.tabProxy')">
            <NCard style="margin-top: 16px">
              <NEmpty :description="$t('servers.detail.proxyEmpty')" />
            </NCard>
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
</style>
