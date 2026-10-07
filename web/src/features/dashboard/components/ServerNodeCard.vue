<script setup lang="ts">
import { NCard, NProgress, NSpace, NTag, NText } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import ServerStatusTag from "@/features/servers/components/ServerStatusTag.vue";
import type { ServerNodeCardModel } from "@/features/dashboard/utils/serverNode";

interface Props {
  card: ServerNodeCardModel;
}

const props = defineProps<Props>();
const { t } = useI18n();

/** containersText renders the container count tag in the display locale. */
const containersText = computed<string>(() =>
  props.card.containerCount === 1
    ? t("dashboard.health.containersOne", { count: 1 })
    : t("dashboard.health.containersOther", { count: props.card.containerCount }),
);
</script>

<template>
  <NCard
    size="small"
    class="node-card"
  >
    <NSpace vertical :size="12" class="node-stack">
      <div class="node-top">
        <div class="node-avatar" aria-hidden="true">
          {{ card.initials }}
        </div>
        <div class="node-head">
          <NText strong>{{ card.name }}</NText>
          <NText depth="3" class="node-sub" :title="card.subtitle">{{ card.subtitle }}</NText>
        </div>
        <ServerStatusTag class="node-status" :status="card.status" />
      </div>
      <div class="node-metrics">
        <div class="node-metric">
          <NText depth="3" class="metric-label">CPU</NText>
          <NText class="num metric-val">
            {{ card.cpu.label }}
          </NText>
          <NProgress
            type="line"
            :percentage="card.cpu.percentage"
            :show-indicator="false"
            :color="card.cpu.color"
          />
        </div>
        <div class="node-metric">
          <NText depth="3" class="metric-label">RAM</NText>
          <NText class="num metric-val">
            {{ card.ram.label }}
          </NText>
          <NProgress
            type="line"
            :percentage="card.ram.percentage"
            :show-indicator="false"
            :color="card.ram.color"
          />
        </div>
        <div class="node-metric">
          <NText depth="3" class="metric-label">Disk</NText>
          <NText class="num metric-val">
            {{ card.disk.label }}
          </NText>
          <NProgress
            type="line"
            :percentage="card.disk.percentage"
            :show-indicator="false"
            :color="card.disk.color"
          />
        </div>
      </div>
      <NSpace align="center" :size="8">
        <NTag v-if="card.containerCount !== null" size="small" :bordered="false">
          {{ containersText }}
        </NTag>
        <NTag v-if="card.arch" size="small" :bordered="false">
          {{ card.arch }}
        </NTag>
        <NTag v-if="card.sshUser" size="small" :bordered="false">
          {{ card.sshUser }}
        </NTag>
      </NSpace>
    </NSpace>
  </NCard>
</template>

<style scoped>
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
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-3);
}

.node-metric {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.metric-label {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.metric-val {
  font-size: var(--text-sm);
  color: var(--fg-2);
}

.num {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}
</style>
