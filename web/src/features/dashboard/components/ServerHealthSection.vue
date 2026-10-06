<script setup lang="ts">
import { NButton, NEmpty, NSkeleton, NSpace, NText } from "naive-ui";
import { computed } from "vue";
import { RouterLink } from "vue-router";
import { useI18n } from "vue-i18n";

import type { ServerNodeCardModel } from "@/features/dashboard/utils/serverNode";
import ServerNodeCard from "./ServerNodeCard.vue";

interface Props {
  cards: ServerNodeCardModel[];
  loading: boolean;
  totalCount: number;
  hiddenCount: number;
}

const props = defineProps<Props>();
const { t } = useI18n();

/** moreNodesText renders the overflow count in the display locale. */
const moreNodesText = computed<string>(() =>
  props.hiddenCount === 1
    ? t("dashboard.health.moreOne", { count: 1 })
    : t("dashboard.health.moreOther", { count: props.hiddenCount }),
);
</script>

<template>
  <div class="section-title">
    <h2>{{ $t("dashboard.health.title") }}</h2>
    <NText depth="3" class="mono meta">
      heartbeat every 10s over gRPC server-authenticated TLS
    </NText>
  </div>
  <NEmpty
    v-if="!loading && totalCount === 0"
    :description="$t('dashboard.health.empty')"
  >
    <template #extra>
      <NSpace vertical :size="8" align="center">
        <NText depth="3">
          {{ $t("dashboard.health.emptyHint") }}
        </NText>
        <RouterLink :to="{ name: 'servers', query: { add: '1' } }" custom>
          <template #default="{ navigate }">
            <NButton type="primary" size="small" @click="navigate">
              {{ $t("dashboard.health.addServer") }}
            </NButton>
          </template>
        </RouterLink>
      </NSpace>
    </template>
  </NEmpty>
  <div v-else class="node-grid">
    <NSkeleton v-if="loading && totalCount === 0" text :repeat="3" />
    <ServerNodeCard
      v-for="card in cards"
      :key="card.id"
      :card="card"
    />
  </div>
  <!-- Cap the grid so a large fleet is not re-diffed in full every 5s
       (B4-14); link out for the rest. -->
  <NText v-if="hiddenCount > 0" depth="3" class="meta">
    {{ moreNodesText }} —
    <RouterLink :to="{ name: 'servers' }">{{ $t("dashboard.health.viewAll") }}</RouterLink>
  </NText>
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

.node-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-4);
}

@media (max-width: 640px) {
  .node-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
