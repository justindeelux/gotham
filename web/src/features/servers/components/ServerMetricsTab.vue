<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NRadioButton,
  NRadioGroup,
  NSpace,
  NSpin,
  NText,
} from "naive-ui";
import { inject } from "vue";
import { useI18n } from "vue-i18n";

import type { MetricStep } from "@/features/servers/api/metrics";
import {
  ServerMetricsKey,
} from "@/features/servers/composables/useServerMetrics";
import type { MetricRefreshChoice } from "@/features/servers/utils/serverMetricsView";
import {
  formatPercentValue,
  formatRateValue,
  metricRangeHint,
} from "@/features/servers/utils/serverMetricsView";
import MetricsChart from "@/shared/ui/MetricsChart.vue";

const metrics = inject(ServerMetricsKey);
if (!metrics) {
  throw new Error("ServerMetricsTab must be used inside ServerDetailPage.");
}

const { locale } = useI18n();

const {
  metricRanges,
  metricRefreshOptions,
  metricStep,
  metricsLoading,
  metricsLoaded,
  metricsError,
  metricsAvailable,
  metricRefresh,
  metricSeriesStep,
  appliedMetricRange,
  hasMetrics,
  metricCharts,
  loadMetrics,
  selectMetricRefresh,
  selectMetricRange,
} = metrics;
</script>

<template>
  <NSpace vertical :size="16" style="margin-top: 16px">
    <NCard v-if="!metricsAvailable" :title="$t('servers.metrics.unavailable')">
      <NEmpty :description="$t('servers.errors.metricsDisabled')" />
    </NCard>

    <template v-else>
      <div class="metrics-toolbar">
        <NRadioGroup
          :value="metricStep"
          size="small"
          @update:value="(value: MetricStep) => selectMetricRange(value)"
        >
          <NRadioButton
            v-for="range in metricRanges"
            :key="range.step"
            :value="range.step"
          >
            {{ range.step }}
          </NRadioButton>
        </NRadioGroup>
        <NText depth="3">
          {{ metricRangeHint(metricStep, locale) }}
          <template v-if="metricsLoaded && metricSeriesStep !== metricStep">
            · {{ $t("servers.metrics.serverStep", { step: metricSeriesStep }) }}
          </template>
        </NText>
        <NSpace align="center" :size="8">
          <NText depth="3">{{ $t("servers.metrics.autoRefresh") }}</NText>
          <NRadioGroup
            :value="metricRefresh"
            size="small"
            :aria-label="$t('servers.metrics.autoRefresh')"
            @update:value="(value: MetricRefreshChoice) => selectMetricRefresh(value)"
          >
            <NRadioButton
              v-for="option in metricRefreshOptions"
              :key="option.value"
              :value="option.value"
            >
              {{ option.label }}
            </NRadioButton>
          </NRadioGroup>
        </NSpace>
        <NText depth="3" style="margin-left: auto">
          {{ $t("servers.metrics.samplesNote") }}
        </NText>
        <NButton
          size="small"
          :loading="metricsLoading"
          @click="void loadMetrics()"
        >
          {{ $t("servers.metrics.refresh") }}
        </NButton>
      </div>

      <NAlert
        v-if="metricsError"
        type="error"
        :show-icon="true"
        data-testid="metrics-error"
      >
        <NSpace align="center" :size="12" wrap>
          <span>{{ metricsError }}</span>
          <NButton size="small" @click="void loadMetrics()">
            {{ $t("servers.metrics.retry") }}
          </NButton>
        </NSpace>
      </NAlert>

      <NSpin :show="metricsLoading">
        <div class="metrics-grid">
          <NCard
            v-for="chart in metricCharts"
            :key="chart.title"
            :title="chart.title"
          >
            <template #header-extra>
              <NText depth="3" class="num">
                {{ chart.latest }}
              </NText>
            </template>
            <div class="metric-chart" :data-chart="chart.title">
              <MetricsChart
                v-if="hasMetrics"
                :series="chart.series"
                :step-ms="appliedMetricRange.stepMs"
                :y-max="chart.percent ? 100 : undefined"
                :format-value="
                  chart.percent ? formatPercentValue : formatRateValue
                "
                :height="150"
              />
              <NEmpty
                v-else
                size="small"
                :description="
                  metricsError
                    ? $t('servers.metrics.emptyError')
                    : metricsLoaded
                      ? $t('servers.metrics.emptyNone')
                      : $t('servers.metrics.emptyLoading')
                "
              />
            </div>
          </NCard>
        </div>
      </NSpin>

      <NText depth="3">
        Values come from the node agent's heartbeats, aggregated by
        <span class="mono">GET /api/v1/servers/{id}/metrics?from&amp;to&amp;step</span>.
        The control plane never invents a point for a bucket the node
        did not report.
      </NText>
    </template>
  </NSpace>
</template>

<style scoped>
.metrics-toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.metrics-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: var(--space-4);
}
</style>
