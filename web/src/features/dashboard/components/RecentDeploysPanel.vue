<script setup lang="ts">
import { NCard, NButton, NEmpty, NSkeleton, NSpace, NText } from "naive-ui";
import { computed, onMounted } from "vue";

import DeploymentStatusTag from "@/features/applications/components/DeploymentStatusTag.vue";
import type { RecentDeployment } from "@/features/applications";
import { commitSubject, durationText, shortCommitSha } from "@/features/applications";
import { useRecentDeploymentsStore } from "@/features/dashboard/stores/deployments";
import GothamIcon from "@/shared/ui/GothamIcon.vue";
import { relativeTime } from "@/shared/utils/format";

const store = useRecentDeploymentsStore();

onMounted(() => {
  void store.fetchRecentDeployments();
});

/** commitLine renders the short hash plus subject, falling back to the branch. */
function commitLine(deployment: RecentDeployment): string {
  if (!deployment.commit_sha) {
    return deployment.application_branch;
  }
  const subject = commitSubject(deployment.commit_message);
  const sha = shortCommitSha(deployment.commit_sha);
  return subject ? `${sha} ${subject}` : sha;
}

/** startedAt is the row's reference time: run start, else creation. */
const startedAt = (deployment: RecentDeployment): string =>
  deployment.started_at ?? deployment.created_at;

const rows = computed<RecentDeployment[]>(() => store.deployments);
</script>

<template>
  <div class="section-title">
    <h2>{{ $t("dashboard.deploys.title") }}</h2>
  </div>
  <NCard size="small">
    <NSkeleton v-if="store.loading" text :repeat="3" />
    <template v-else-if="store.error">
      <NText depth="3">{{ store.error }}</NText>
      <div class="panel-action">
        <NButton size="small" quaternary @click="() => void store.fetchRecentDeployments()">
          {{ $t("dashboard.deploys.retry") }}
        </NButton>
      </div>
    </template>
    <NEmpty
      v-else-if="rows.length === 0"
      :description="$t('dashboard.deploys.empty')"
    >
      <template #icon>
        <GothamIcon name="history" />
      </template>
      <template #extra>
        <NText depth="3">
          {{ $t("dashboard.deploys.emptyHint") }}
        </NText>
      </template>
    </NEmpty>
    <NSpace v-else vertical :size="12">
      <div v-for="deployment in rows" :key="deployment.id" class="deploy-row">
        <div class="deploy-main">
          <NText strong class="deploy-app">{{ deployment.application_name }}</NText>
          <NText depth="3" class="mono deploy-commit" :title="deployment.commit_message">
            {{ commitLine(deployment) }}
          </NText>
        </div>
        <div class="deploy-meta">
          <NText depth="3" class="mono">{{ durationText(deployment) }}</NText>
          <NText depth="3">{{ relativeTime(startedAt(deployment)) }}</NText>
          <DeploymentStatusTag :state="deployment.state" />
        </div>
      </div>
    </NSpace>
  </NCard>
</template>

<style scoped>
.section-title {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.section-title h2 {
  font-size: var(--text-xl);
  color: var(--fg-2);
  margin: 0;
}

.mono {
  font-family: var(--font-mono);
}

.panel-action {
  margin-top: var(--space-2);
}

.deploy-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.deploy-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.deploy-app {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.deploy-commit {
  font-size: var(--text-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.deploy-meta {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--text-xs);
  flex: 0 0 auto;
}
</style>
