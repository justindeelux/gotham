<script setup lang="ts">
import { NCard, NEmpty, NSpace, NText } from "naive-ui";

/**
 * Deployment is the typed seam for a deployments store that does not exist
 * yet. The recent-deploys widget renders an explicit empty state until that
 * store exists — never fabricated rows.
 *
 * TODO: add features/dashboard/stores/deployments.ts backed by the deployments API,
 * replace `deployments` below with live data, and remove the empty state.
 */
interface Deployment {
  id: string;
  appName: string;
  repo: string;
  branch: string;
  commit: string;
  trigger: string;
  duration: string;
  status: "running" | "failed" | "paused";
}

/** No backend serves deployments yet, so the list is always empty. */
const deployments: Deployment[] = [];
</script>

<template>
  <div class="section-title">
    <h2>Recent deploys</h2>
    <NText depth="3" class="mono meta">source: deployments · realtime via Redis</NText>
  </div>
  <NCard size="small">
    <NEmpty
      v-if="deployments.length === 0"
      description="No deployments yet"
    >
      <template #extra>
        <NText depth="3">
          Push an application to see build history, durations, and
          statuses here.
        </NText>
      </template>
    </NEmpty>
    <NSpace vertical :size="12">
      <div class="card-foot">
        <NText depth="3">Queue: no data yet</NText>
        <NText depth="3">No build history to show</NText>
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

.meta {
  font-size: var(--text-xs);
  color: var(--muted);
}

.card-foot {
  display: flex;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
}
</style>
