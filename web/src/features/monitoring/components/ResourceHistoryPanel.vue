<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NRadioButton,
  NRadioGroup,
  NSelect,
  NSpace,
  NSpin,
  NTag,
  NText,
} from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type {
  ResourceHistory,
  ResourceHistoryRange,
  ResourceType,
} from "@/features/monitoring/api/resourceHistory";
import { getResourceHistory } from "@/features/monitoring/api/resourceHistory";
import {
  buildResourceCharts,
  chartStepMs,
  formatCores,
  formatRate,
  memoryLimitLabel,
  resourceRanges,
  serviceOptions,
  shortId,
} from "@/features/monitoring/utils/resourceHistoryView";
import MetricsChart from "@/shared/ui/MetricsChart.vue";
import type { ChartMarker } from "@/shared/ui/MetricsChart.vue";
import { formatBytes, relativeTime } from "@/shared/utils/format";

interface Props {
  resourceType: ResourceType;
  resourceId: string;
}

const props = defineProps<Props>();
const { t } = useI18n();

const range = ref<ResourceHistoryRange>("1h");
const service = ref("");
const services = ref<Array<{ label: string; value: string }>>([]);
const history = ref<ResourceHistory | null>(null);
const loading = ref(false);
const loaded = ref(false);
const error = ref<string | null>(null);
let requestToken = 0;

const charts = computed(() => buildResourceCharts(history.value));
const stepMs = computed(() => chartStepMs(history.value));
const markers = computed<ChartMarker[]>(() =>
  (history.value?.markers ?? []).map((marker) => ({
    at: new Date(marker.at).getTime(),
    label: shortId(marker.deployment_id),
  })),
);
const limitText = computed(() => memoryLimitLabel(history.value));
const unavailable = computed(
  () => loaded.value && history.value?.capability === "unavailable",
);
const partial = computed(() => history.value?.coverage === "partial");

const serviceChoices = computed(() => [
  { label: t("monitoring.resourceHistory.allServices"), value: "" },
  ...services.value,
]);

async function load(): Promise<void> {
  if (!props.resourceId) {
    return;
  }
  const token = ++requestToken;
  loading.value = true;
  error.value = null;
  try {
    const result = await getResourceHistory(
      props.resourceType,
      props.resourceId,
      range.value,
      service.value,
    );
    if (token !== requestToken) {
      return;
    }
    history.value = result;
    loaded.value = true;
    if (service.value === "") {
      services.value = serviceOptions(result);
    }
  } catch (failure) {
    if (token !== requestToken) {
      return;
    }
    error.value = failure instanceof Error ? failure.message : String(failure);
  } finally {
    if (token === requestToken) {
      loading.value = false;
    }
  }
}

function selectRange(value: ResourceHistoryRange): void {
  if (value === range.value) {
    return;
  }
  range.value = value;
  void load();
}

function selectService(value: string): void {
  service.value = value;
  void load();
}

function chartFormatter(key: string): (_value: number) => string {
  if (key === "cpu") {
    return formatCores;
  }
  if (key === "memory") {
    return formatBytes;
  }
  return formatRate;
}

watch(
  () => [props.resourceType, props.resourceId],
  () => {
    history.value = null;
    loaded.value = false;
    services.value = [];
    service.value = "";
    void load();
  },
  { immediate: true },
);
</script>

<template>
  <NSpace
    vertical
    :size="16"
    style="margin-top: 16px"
    data-testid="resource-history"
  >
    <div class="resource-history__toolbar">
      <NRadioGroup
        :value="range"
        size="small"
        :aria-label="t('monitoring.resourceHistory.rangeLabel')"
        @update:value="selectRange"
      >
        <NRadioButton
          v-for="option in resourceRanges"
          :key="option.value"
          :value="option.value"
        >
          {{ option.label }}
        </NRadioButton>
      </NRadioGroup>
      <NSelect
        v-if="serviceChoices.length > 1"
        :value="service"
        size="small"
        class="resource-history__service"
        :options="serviceChoices"
        :aria-label="t('monitoring.resourceHistory.serviceLabel')"
        @update:value="selectService"
      />
      <NTag v-if="partial" type="warning" size="small" data-testid="resource-history-partial">
        {{ t("monitoring.resourceHistory.partial") }}
      </NTag>
      <NText depth="3">
        {{ t("monitoring.resourceHistory.seriesCount", { count: history?.series.length ?? 0 }) }}
      </NText>
      <NButton
        size="small"
        :loading="loading"
        style="margin-left: auto"
        @click="void load()"
      >
        {{ t("monitoring.resourceHistory.refresh") }}
      </NButton>
    </div>

    <NAlert v-if="error" type="error" :show-icon="true" data-testid="resource-history-error">
      <NSpace align="center" :size="12" wrap>
        <span>{{ error }}</span>
        <NButton size="small" @click="void load()">
          {{ t("monitoring.resourceHistory.retry") }}
        </NButton>
      </NSpace>
    </NAlert>

    <NSpin :show="loading">
      <NCard v-if="unavailable" :title="t('monitoring.resourceHistory.unavailableTitle')">
        <NEmpty :description="t('monitoring.resourceHistory.unavailableBody')" />
      </NCard>
      <NEmpty
        v-else-if="loaded && (history?.series.length ?? 0) === 0"
        :description="t('monitoring.resourceHistory.emptyRange')"
      />
      <div v-else class="resource-history__grid">
        <NCard
          v-for="chart in charts"
          :key="chart.key"
          :title="t(`monitoring.resourceHistory.charts.${chart.key}`)"
        >
          <template #header-extra>
            <NSpace align="center" :size="8">
              <NText depth="3" class="num">{{ chart.latest }}</NText>
              <NText v-if="chart.latestAt" depth="3" class="resource-history__observed">
                {{ relativeTime(chart.latestAt) }}
              </NText>
            </NSpace>
          </template>
          <div class="resource-history__chart" :data-chart="chart.key">
            <MetricsChart
              :series="chart.series"
              :step-ms="stepMs"
              :markers="markers"
              :format-value="chartFormatter(chart.key)"
              :y-max="undefined"
              :height="150"
            />
          </div>
          <NText
            v-if="chart.key === 'memory' && limitText"
            depth="3"
            class="resource-history__limit"
          >
            {{ t("monitoring.resourceHistory.memoryLimit", { limit: limitText }) }}
          </NText>
        </NCard>
      </div>
    </NSpin>

    <NText depth="3" class="resource-history__note">
      {{ t("monitoring.resourceHistory.units") }}
    </NText>
  </NSpace>
</template>

<style scoped>
.resource-history__toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.resource-history__service {
  min-width: 160px;
  max-width: 240px;
}

.resource-history__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: var(--space-4);
}

.resource-history__chart {
  overflow: hidden;
}

.resource-history__observed,
.resource-history__limit,
.resource-history__note {
  font-size: var(--text-xs);
}

.resource-history__limit {
  display: block;
  margin-top: var(--space-2);
}

@media (max-width: 640px) {
  .resource-history__service {
    min-width: 120px;
    max-width: none;
    flex: 1;
  }
}
</style>
