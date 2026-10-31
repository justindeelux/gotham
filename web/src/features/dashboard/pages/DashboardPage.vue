<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NSkeleton,
  NSpace,
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
import type { Server } from "@/features/servers";
import { useAuthStore } from "@/features/auth";
import { useIncidentsStore } from "@/features/incidents/stores/incidents";
import { useServersStore } from "@/features/servers";
import { applicationTileView, buildApplicationTileInput } from "@/features/dashboard/utils/dashboard";
import type { ServerNodeCardModel } from "@/features/dashboard/utils/serverNode";
import { buildServerNodeCard } from "@/features/dashboard/utils/serverNode";
import DashboardHeader from "@/features/dashboard/components/DashboardHeader.vue";
import DashboardProjectsSection from "@/features/dashboard/components/DashboardProjectsSection.vue";
import RecentDeploysPanel from "@/features/dashboard/components/RecentDeploysPanel.vue";
import ServerHealthSection from "@/features/dashboard/components/ServerHealthSection.vue";
import NeedsAttentionSection from "@/features/incidents/components/NeedsAttentionSection.vue";
import GothamIcon from "@/shared/ui/GothamIcon.vue";

const authStore = useAuthStore();
const incidentsStore = useIncidentsStore();
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
    applicationsError.value = "dashboard.status.appsLoadFailed";
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

const showAddServer = computed<boolean>(
  () =>
    authStore.isOwner &&
    !serversStore.loading &&
    serversStore.error === null &&
    totalCount.value === 0,
);

const isFreshInstall = computed<boolean>(
  () =>
    showAddServer.value &&
    !applicationsLoading.value &&
    applicationsError.value === null &&
    applicationTotal.value === 0,
);

const attentionCount = computed<number>(() =>
  Math.max(
    incidentsStore.state === "ready" ? incidentsStore.activeCount : 0,
    authStore.isOwner ? offlineServers.value.length : 0,
  ),
);

type DashboardStatus =
  | "loading"
  | "unavailable"
  | "attention"
  | "noServers"
  | "starting"
  | "clear";

const statusState = computed<DashboardStatus>(() => {
  if (
    incidentsStore.state === "idle" ||
    incidentsStore.state === "loading" ||
    (authStore.isOwner && serversStore.loading)
  ) {
    return "loading";
  }
  if (attentionCount.value > 0) {
    return "attention";
  }
  if (incidentsStore.state !== "ready") {
    return "unavailable";
  }
  if (authStore.isOwner && serversStore.error !== null) {
    return "unavailable";
  }
  if (authStore.isOwner && totalCount.value === 0) {
    return "noServers";
  }
  if (authStore.isOwner && readyCount.value < totalCount.value) {
    return "starting";
  }
  return "clear";
});

const statusText = computed<string>(() => {
  switch (statusState.value) {
    case "attention":
      return attentionCount.value === 1
        ? t("dashboard.status.attentionOne", { count: 1 })
        : t("dashboard.status.attentionOther", { count: attentionCount.value });
    case "unavailable":
      return t("dashboard.status.unavailable");
    case "noServers":
      return t("dashboard.status.noServers");
    case "starting":
      return t("dashboard.status.starting");
    case "clear":
      return t("dashboard.status.allClear");
    default:
      return "";
  }
});

const nodeWidgetLimit = 6;
const visibleNodes = computed<Server[]>(() =>
  servers.value.slice(0, nodeWidgetLimit),
);
const hiddenNodeCount = computed<number>(() =>
  Math.max(0, servers.value.length - nodeWidgetLimit),
);

const nodeCards = computed<ServerNodeCardModel[]>(() =>
  visibleNodes.value.map((server) => buildServerNodeCard(server)),
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
    <DashboardHeader :show-add-server="showAddServer" />

    <div v-if="authStore.isOwner && serversStore.error" class="dash-alert">
      <NAlert type="error" :show-icon="true">
        {{ serversStore.error }}
      </NAlert>
    </div>

    <NCard
      v-if="isFreshInstall"
      size="small"
      class="dash-first-run"
      data-testid="dashboard-first-run"
    >
      <NEmpty :description="$t('dashboard.empty.title')">
        <template #icon>
          <GothamIcon name="server" />
        </template>
        <template #extra>
          <NSpace vertical :size="12" align="center">
            <NText depth="3">{{ $t("dashboard.empty.body") }}</NText>
            <RouterLink :to="{ name: 'servers', query: { add: '1' } }" custom>
              <template #default="{ navigate }">
                <NButton type="primary" @click="navigate">
                  {{ $t("dashboard.empty.action") }}
                </NButton>
              </template>
            </RouterLink>
          </NSpace>
        </template>
      </NEmpty>
    </NCard>

    <template v-else>
      <NCard size="small" data-test="status">
        <div class="status-inner">
          <div class="status-sentence">
            <NSkeleton
              v-if="statusState === 'loading'"
              text
              class="status-skeleton"
            />
            <h2
              v-else
              class="status-heading"
              data-testid="dashboard-status"
            >
              {{ statusText }}
            </h2>
          </div>

          <div class="status-figures">
            <RouterLink
              v-if="authStore.isOwner"
              :to="{ name: 'servers' }"
              class="status-figure"
              data-testid="dashboard-servers-figure"
            >
              <NSkeleton
                v-if="serversStore.loading && totalCount === 0"
                text
                class="figure-skeleton"
              />
              <template v-else>
                <span class="figure-value">
                  {{ readyCount }}<span class="figure-unit">/{{ totalCount }}</span>
                </span>
                <span class="figure-label">{{ $t("dashboard.status.serversReady") }}</span>
              </template>
            </RouterLink>

            <div class="status-figure" data-testid="dashboard-apps-figure">
              <NSkeleton
                v-if="tile.state === 'loading'"
                text
                class="figure-skeleton"
              />
              <template v-else-if="tile.state === 'error'">
                <span class="figure-value">—</span>
                <span class="figure-label">{{ tile.error }}</span>
                <NButton
                  size="tiny"
                  quaternary
                  @click="() => void fetchApplicationCounts()"
                >
                  {{ $t("dashboard.status.retry") }}
                </NButton>
              </template>
              <RouterLink
                v-else
                :to="{ name: 'projects' }"
                class="figure-link"
              >
                <span class="figure-value">
                  {{ tile.runningText }}
                </span>
                <span class="figure-label">{{ $t("dashboard.status.appsRunning") }}</span>
              </RouterLink>
              <NText
                v-if="tile.state !== 'loading' && tile.state !== 'error' && tile.hint"
                depth="3"
                class="figure-hint"
              >
                {{ tile.hint }}
              </NText>
            </div>
          </div>
        </div>
      </NCard>

      <NeedsAttentionSection />

      <DashboardProjectsSection />

      <ServerHealthSection
        v-if="authStore.isOwner && totalCount > 0"
        :cards="nodeCards"
        :hidden-count="hiddenNodeCount"
      />

      <RecentDeploysPanel />
    </template>
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

.status-inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.status-sentence {
  min-width: 0;
}

.status-heading {
  margin: 0;
  font-size: var(--text-lg);
  font-weight: 600;
  color: var(--fg-2);
  line-height: var(--leading-title);
}

.status-skeleton {
  max-width: 18rem;
}

.status-figures {
  display: flex;
  align-items: flex-start;
  gap: var(--space-6);
  flex-wrap: wrap;
}

.status-figure {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.status-figure,
.figure-link {
  color: var(--fg);
  text-decoration: none;
}

.status-figure:hover .figure-value,
.figure-link:hover .figure-value {
  color: var(--accent-ink);
}

.figure-link {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.figure-value {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--text-2xl);
  line-height: var(--leading-title);
  color: var(--fg-2);
}

.figure-unit {
  font-size: var(--text-base);
  color: var(--muted);
}

.figure-label {
  font-size: var(--text-xs);
  color: var(--muted);
}

.figure-hint {
  font-size: var(--text-xs);
}

.figure-skeleton {
  max-width: 4rem;
}
</style>
