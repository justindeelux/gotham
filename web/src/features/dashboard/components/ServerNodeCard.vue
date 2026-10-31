<script setup lang="ts">
import { NCard, NSpace, NTag, NText } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";

import ServerStatusTag from "@/features/servers/components/ServerStatusTag.vue";
import ServerUsageSegments from "@/features/servers/components/ServerUsageSegments.vue";
import { isUsageStale } from "@/features/servers";
import type { ServerNodeCardModel } from "@/features/dashboard/utils/serverNode";

interface Props {
  card: ServerNodeCardModel;
}

const props = defineProps<Props>();
const { t } = useI18n();

const containersText = computed<string>(() =>
  props.card.containerCount === 1
    ? t("dashboard.health.containersOne", { count: 1 })
    : t("dashboard.health.containersOther", { count: props.card.containerCount }),
);
const stale = computed<boolean>(() => isUsageStale(props.card.lastSeen));
</script>

<template>
  <RouterLink
    class="node-card-link"
    :to="{ name: 'server-detail', params: { id: props.card.id } }"
    :aria-label="t('dashboard.health.openServer', { name: props.card.name })"
  >
    <NCard size="small" hoverable class="node-card">
      <NSpace vertical :size="12" class="node-stack">
        <div class="node-top">
          <div class="node-avatar" aria-hidden="true">
            {{ props.card.initials }}
          </div>
          <div class="node-head">
            <NText strong>{{ props.card.name }}</NText>
            <NText depth="3" class="node-sub" :title="props.card.subtitle">{{ props.card.subtitle }}</NText>
          </div>
          <ServerStatusTag class="node-status" :status="props.card.status" />
        </div>
        <div class="node-metrics">
          <ServerUsageSegments
            :label="$t('servers.overview.cpuUsage')"
            :value="props.card.cpuUsage"
            :stale="stale"
          />
          <ServerUsageSegments
            :label="$t('servers.overview.memUsage')"
            :value="props.card.memUsage"
            :stale="stale"
          />
          <ServerUsageSegments
            :label="$t('servers.overview.diskUsage')"
            :value="props.card.diskUsage"
            :stale="stale"
          />
        </div>
        <NSpace align="center" :size="8">
          <NTag v-if="props.card.containerCount !== null" size="small" :bordered="false">
            {{ containersText }}
          </NTag>
          <NTag v-if="props.card.arch" size="small" :bordered="false">
            {{ props.card.arch }}
          </NTag>
          <NTag v-if="props.card.sshUser" size="small" :bordered="false">
            {{ props.card.sshUser }}
          </NTag>
        </NSpace>
      </NSpace>
    </NCard>
  </RouterLink>
</template>

<style scoped>
.node-card-link {
  display: block;
  min-width: 0;
  border-radius: var(--radius-md);
  color: inherit;
  text-decoration: none;
}

.node-card-link:focus-visible {
  outline: none;
  box-shadow: var(--focus-ring);
}

.node-stack {
  min-width: 0;
}

.node-top {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.node-status {
  flex: 0 0 auto;
}

.node-avatar {
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-md);
  background: var(--accent-softer);
  color: var(--accent-ink);
  font-weight: 700;
  font-size: var(--text-sm);
}

.node-head {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1 1 auto;
}

.node-sub {
  font-size: var(--text-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.node-metrics {
  display: grid;
  gap: var(--space-3);
  min-width: 0;
}
</style>
