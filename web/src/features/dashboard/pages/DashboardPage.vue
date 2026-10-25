<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NSkeleton,
  NText,
} from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { RouterLink } from "vue-router";
import { useI18n } from "vue-i18n";

import {
  countRunning,
  latestDeploymentStates,
  listApplications,
} from "@/features/applications";
import type { Server, ServerStatus } from "@/features/servers";
import { useAuthStore } from "@/features/auth";
import { useServersStore } from "@/features/servers";
import { applicationTileView, buildApplicationTileInput } from "@/features/dashboard/utils/dashboard";
import type { ServerNodeCardModel } from "@/features/dashboard/utils/serverNode";
import { buildServerNodeCard } from "@/features/dashboard/utils/serverNode";
import { usageView } from "@/shared/utils/format";
import DashboardAside from "@/features/dashboard/components/DashboardAside.vue";
import DashboardHeader from "@/features/dashboard/components/DashboardHeader.vue";
import RecentDeploysPanel from "@/features/dashboard/components/RecentDeploysPanel.vue";
import ServerHealthSection from "@/features/dashboard/components/ServerHealthSection.vue";
import ServersReadyKpi from "@/features/dashboard/components/ServersReadyKpi.vue";
import GothamIcon from "@/shared/ui/GothamIcon.vue";

const authStore = useAuthStore();
const serversStore = useServersStore();
const { locale, t } = useI18n();

const applicationsLoading = ref(true);
const applicationsError = ref<string | null>(null);
const applicationFailedReads = ref(0);
const applicationTotal = ref(0);
const applicationRunning = ref(0);

async function fetchApplicationCounts(): Promise<void> {
  applicationsLoading.value = true;
  applicationsError.value = null;
  try {
    const applications = await listApplications();
    applicationTotal.value = applications.length;
    if (applications.length === 0) {
      applicationRunning.value = 0;
      applicationFailedReads.value = 0;
      return;
    }
    const { states, failed } = await latestDeploymentStates(
      applications.map((application) => application.id),
    );
    applicationRunning.value = countRunning(states);
    applicationFailedReads.value = failed;
  } catch {
    applicationTotal.value = 0;
    applicationRunning.value = 0;
    applicationFailedReads.value = 0;
    applicationsError.value = "dashboard.kpi.loadFailed";
  } finally {
    applicationsLoading.value = false;
  }
}

const tile = computed(() =>
  applicationTileView(
    buildApplicationTileInput({
      loading: applicationsLoading.value,
      error: applicationsError.value ? t(applicationsError.value) : null,
      total: applicationTotal.value,
      running: applicationRunning.value,
      failedReads: applicationFailedReads.value,
    }),
    locale.value,
  ),
);

const servers = computed<Server[]>(() => serversStore.servers);
const readyCount = computed<number>(
  () => servers.value.filter((server) => server.status === "ready").length,
);
const totalCount = computed<number>(() => servers.value.length);

const offlineServers = computed<Server[]>(() =>
  servers.value.filter(
    (server) => server.status === "offline" || server.status === "error",
  ),
);

const aggregateStatus = computed<ServerStatus>(() => {
  if (offlineServers.value.length > 0) {
    return "offline";
  }
  if (readyCount.value > 0) {
    return "ready";
  }
  return "pending";
});

const nodeWidgetLimit = 6;
const visibleNodes = computed<Server[]>(() =>
  servers.value.slice(0, nodeWidgetLimit),
);
const hiddenNodeCount = computed<number>(() =>
  Math.max(0, servers.value.length - nodeWidgetLimit),
);

const nodeCards = computed<ServerNodeCardModel[]>(() =>
  visibleNodes.value.map((server) =>
    buildServerNodeCard(server, {
      cpu: usageView(server.cpu_usage, "var(--accent)"),
      ram: usageView(server.mem_usage, "var(--success)"),
      disk: usageView(server.disk_usage, "var(--success)"),
    }),
  ),
);

onMounted(() => {
  if (authStore.isOwner) {
    void serversStore.fetchServers().catch(() => {
    });
    serversStore.pollServers();
  }
  void fetchApplicationCounts();
});
</script>

<template>
  <div class="dash">
    <DashboardHeader />

    <div v-if="authStore.isOwner && serversStore.error" class="dash-alert">
      <NAlert type="error" :show-icon="true">
        {{ serversStore.error }}
      </NAlert>
    </div>

    <div class="kpi-row">
      <ServersReadyKpi
        v-if="authStore.isOwner"
        :loading="serversStore.loading"
        :ready-count="readyCount"
        :total-count="totalCount"
        :aggregate-status="aggregateStatus"
        :offline-names="offlineServers.map((server) => server.name).join(', ')"
      />

      <NCard class="kpi" :title="$t('dashboard.kpi.applications')" size="small">
        <NSkeleton v-if="tile.state === 'loading'" text :repeat="2" />
        <template v-else-if="tile.state === 'error'">
          <p class="kpi-value num">—</p>
          <p class="kpi-sub">
            <NText depth="3">{{ tile.error }}</NText>
            <NButton
              size="small"
              quaternary
              @click="() => void fetchApplicationCounts()"
            >
              {{ $t("dashboard.kpi.retry") }}
            </NButton>
          </p>
        </template>
        <template v-else-if="tile.state === 'ready'">
          <p class="kpi-value num">{{ tile.countText }}</p>
          <p class="kpi-sub">
            <RouterLink :to="{ name: 'projects' }">{{ $t("dashboard.kpi.viewProjects") }}</RouterLink>
            <NText v-if="tile.hint" depth="3">{{ tile.hint }}</NText>
          </p>
        </template>
        <NEmpty v-else size="small" :description="$t('dashboard.kpi.noApplications')">
          <template #icon>
            <GothamIcon name="grid" />
          </template>
        </NEmpty>
      </NCard>

      <NCard class="kpi" :title="$t('dashboard.kpi.deploys')" size="small">
        <NEmpty size="small" :description="$t('dashboard.kpi.noDeploys')">
          <template #icon>
            <GothamIcon name="rocket" />
          </template>
        </NEmpty>
      </NCard>

      <NCard class="kpi" :title="$t('dashboard.kpi.ssl')" size="small">
        <NEmpty size="small" :description="$t('dashboard.kpi.noSsl')">
          <template #icon>
            <GothamIcon name="shield" />
          </template>
        </NEmpty>
      </NCard>
    </div>

    <div class="dash-split">
      <div class="dash-main">
        <RecentDeploysPanel />

        <ServerHealthSection
          v-if="authStore.isOwner"
          :cards="nodeCards"
          :loading="serversStore.loading"
          :total-count="totalCount"
          :hidden-count="hiddenNodeCount"
        />
      </div>

      <aside v-if="authStore.isOwner" class="dash-aside">
        <DashboardAside />
      </aside>
    </div>
  </div>
</template>

<style scoped>
.dash {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  max-width: 1360px;
}

.dash-alert {
  max-width: 720px;
}

.kpi-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--space-4);
}

.kpi-value {
  font-size: var(--text-3xl);
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.kpi-sub {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin: 0;
  flex-wrap: wrap;
}

.dash-split {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 360px);
  gap: var(--space-5);
  align-items: start;
}

.dash-main {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-width: 0;
}

.mono {
  font-family: var(--font-mono);
}

.num {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}

.meta {
  font-size: var(--text-xs);
  color: var(--muted);
}

.dash-aside {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-width: 0;
}

@media (max-width: 1180px) {
  .dash-split {
    grid-template-columns: minmax(0, 1fr);
  }
  .kpi-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .kpi-row {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
