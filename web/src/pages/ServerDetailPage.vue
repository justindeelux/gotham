<script setup lang="ts">
import {
  NAlert,
  NAvatar,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NPopconfirm,
  NRadioButton,
  NRadioGroup,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
  NText,
  useMessage,
} from "naive-ui";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";

import type { Server } from "../api/servers";
import { describeServerError, getServer } from "../api/servers";
import type { MetricPoint, MetricStep } from "../api/metrics";
import {
  describeMetricsError,
  getServerMetrics,
  isMetricsDisabled,
} from "../api/metrics";
import type { ChartSeries } from "../components/MetricsChart.vue";
import MetricsChart from "../components/MetricsChart.vue";
import ServerStatusTag from "../components/ServerStatusTag.vue";
import { useMediaQuery } from "../composables/useMediaQuery";
import { useServersStore } from "../stores/servers";
import { formatBytes, relativeTime, toPercent } from "../utils/format";

const route = useRoute();
const router = useRouter();
const message = useMessage();
const serversStore = useServersStore();

const serverId = computed<string>(() => String(route.params.id ?? ""));

const server = ref<Server | null>(null);
const loading = ref(false);
const error = ref<string | null>(null);
const validating = ref(false);
const deleting = ref(false);
const activeTab = ref("overview");

/** One metrics range: the API step and the window it is shown over. */
interface MetricRange {
  step: MetricStep;
  /** Bucket width of the step, in milliseconds. */
  stepMs: number;
  /** Window read at this step; capped by ParseMetricsQuery per step. */
  windowMs: number;
  /** Shown above the charts. */
  hint: string;
}

const metricRanges: MetricRange[] = [
  { step: "1m", stepMs: 60_000, windowMs: 60 * 60_000, hint: "Step 1m · last hour" },
  {
    step: "1h",
    stepMs: 60 * 60_000,
    windowMs: 24 * 60 * 60_000,
    hint: "Step 1h · last 24 hours",
  },
  {
    step: "1d",
    stepMs: 24 * 60 * 60_000,
    windowMs: 7 * 24 * 60 * 60_000,
    hint: "Step 1d · last 7 days",
  },
];

const metricStep = ref<MetricStep>("1m");
const metricPoints = ref<MetricPoint[]>([]);
const metricsLoading = ref(false);
const metricsLoaded = ref(false);
const metricsError = ref<string | null>(null);
/** False once the API answers the FEATURE_METRICS 404: the charts are hidden. */
const metricsAvailable = ref(true);

/** Auto-refresh choices: off unless the operator picks a cadence. */
type MetricRefreshChoice = "off" | "15s" | "60s";
const metricRefreshOptions: Array<{ label: string; value: MetricRefreshChoice }> =
  [
    { label: "off", value: "off" },
    { label: "15s", value: "15s" },
    { label: "60s", value: "60s" },
  ];
const metricRefresh = ref<MetricRefreshChoice>("off");

/** Cadence of the auto-refresh in milliseconds; 0 keeps it off. */
const metricRefreshMs = computed<number>(() => {
  switch (metricRefresh.value) {
    case "15s":
      return 15_000;
    case "60s":
      return 60_000;
    default:
      return 0;
  }
});

/** Interval handle of the auto-refresh; null while it is off. */
let metricsRefreshTimer: ReturnType<typeof setInterval> | null = null;

/**
 * The step the rendered points belong to. It is only written from a
 * successful response, so the chart's gap width and range label always
 * describe the data on screen — never a range the operator merely selected.
 */
const metricSeriesStep = ref<MetricStep>("1m");

/** Token of the newest metrics request; a stale response never writes state. */
let metricsRequestToken = 0;

const activeMetricRange = computed<MetricRange>(
  () => metricRanges.find((range) => range.step === metricStep.value) ?? metricRanges[0],
);

/** appliedMetricRange is the range the rendered points actually belong to. */
const appliedMetricRange = computed<MetricRange>(
  () =>
    metricRanges.find((range) => range.step === metricSeriesStep.value) ??
    metricRanges[0],
);

/** hasMetrics reports whether the loaded window carries any sample. */
const hasMetrics = computed<boolean>(() => metricPoints.value.length > 0);

/**
 * usage reads a 0..1 usage fraction as a percentage. The heartbeat contract
 * stores usage as a fraction (see toPercent), which is what the rollup keeps.
 */
function usage(value: number): number {
  return toPercent(value);
}

/** formatPercentValue renders a percentage axis label. */
function formatPercentValue(value: number): string {
  return `${Math.round(value)}%`;
}

/** formatRateValue renders a byte-per-second axis label. */
function formatRateValue(value: number): string {
  return `${formatBytes(value)}/s`;
}

/** One rendered card: its series, axis and newest reading. */
interface MetricChart {
  title: string;
  /** Percentage charts share a fixed 0..100 axis. */
  percent: boolean;
  series: ChartSeries[];
  /** The newest sample, rendered next to the title. */
  latest: string;
}

/**
 * metricCharts maps the window's points onto the four real charts. Nothing is
 * synthesized: every series is a projection of the returned `points`.
 */
const metricCharts = computed<MetricChart[]>(() => {
  const pointsOf = (select: (point: MetricPoint) => number): ChartSeries["points"] =>
    metricPoints.value.map((point) => ({
      at: new Date(point.bucket).getTime(),
      value: select(point),
    }));
  const last = metricPoints.value[metricPoints.value.length - 1];
  const rx = last ? formatRateValue(last.net_rx_bps) : "";
  const tx = last ? formatRateValue(last.net_tx_bps) : "";
  const read = last ? formatRateValue(last.disk_read_bps) : "";
  const write = last ? formatRateValue(last.disk_write_bps) : "";
  return [
    {
      title: "CPU",
      percent: true,
      series: [
        { name: "CPU", color: "var(--accent)", points: pointsOf((p) => usage(p.cpu_usage)) },
      ],
      latest: last ? formatPercentValue(usage(last.cpu_usage)) : "",
    },
    {
      title: "RAM",
      percent: true,
      series: [
        { name: "RAM", color: "var(--success)", points: pointsOf((p) => usage(p.mem_usage)) },
      ],
      latest: last ? formatPercentValue(usage(last.mem_usage)) : "",
    },
    {
      title: "Disk I/O",
      percent: false,
      series: [
        { name: "Read", color: "var(--accent)", points: pointsOf((p) => p.disk_read_bps) },
        { name: "Write", color: "var(--warn)", points: pointsOf((p) => p.disk_write_bps) },
      ],
      latest: last ? `${read} ↓ · ${write} ↑` : "",
    },
    {
      title: "Network",
      percent: false,
      series: [
        { name: "RX", color: "var(--accent)", points: pointsOf((p) => p.net_rx_bps) },
        { name: "TX", color: "var(--warn)", points: pointsOf((p) => p.net_tx_bps) },
      ],
      latest: last ? `${rx} ↓ · ${tx} ↑` : "",
    },
  ];
});

/** isMetricStep narrows a server-returned step onto the accepted values. */
function isMetricStep(value: string): value is MetricStep {
  return value === "1m" || value === "1h" || value === "1d";
}

/**
 * loadMetrics reads the active step's window. Completion is guarded by both a
 * request generation and the server ID, so a late response for another range
 * or another node can never overwrite the current one. A feature-flag 404
 * hides the charts instead of rendering an error; any other failure surfaces
 * an explicit error with a retry. A quiet call (auto-refresh) never toggles
 * the spinner: a background tick must not flash the loading state.
 */
async function loadMetrics(options: { quiet?: boolean } = {}): Promise<void> {
  const requestServerId = serverId.value;
  if (!requestServerId) {
    return;
  }
  const quiet = options.quiet === true;
  const range = activeMetricRange.value;
  const token = ++metricsRequestToken;
  const isCurrent = (): boolean =>
    token === metricsRequestToken && serverId.value === requestServerId;

  if (!quiet) {
    metricsLoading.value = true;
  }
  metricsError.value = null;
  try {
    const to = new Date();
    const from = new Date(to.getTime() - range.windowMs);
    const series = await getServerMetrics(requestServerId, from, to, range.step);
    if (!isCurrent()) {
      return;
    }
    metricPoints.value = series.points;
    // The response names the aggregation it applied; the chart's range and
    // gap width follow it, falling back to the requested step when a server
    // ever answers an unknown one.
    metricSeriesStep.value = isMetricStep(series.step) ? series.step : range.step;
    metricsLoaded.value = true;
    metricsAvailable.value = true;
  } catch (error) {
    if (!isCurrent()) {
      return;
    }
    if (isMetricsDisabled(error)) {
      metricPoints.value = [];
      metricsLoaded.value = false;
      metricsAvailable.value = false;
      return;
    }
    metricsError.value = describeMetricsError(error);
  } finally {
    // Only the newest request owns the spinner; an obsolete one must not clear
    // a loading state the current request still needs.
    if (isCurrent() && !quiet) {
      metricsLoading.value = false;
    }
  }
}

/** selectMetricRefresh switches the auto-refresh cadence. */
function selectMetricRefresh(choice: MetricRefreshChoice): void {
  metricRefresh.value = choice;
}

/** stopMetricRefresh clears the interval; safe when it is already off. */
function stopMetricRefresh(): void {
  if (metricsRefreshTimer !== null) {
    clearInterval(metricsRefreshTimer);
    metricsRefreshTimer = null;
  }
}

/**
 * syncMetricRefresh starts or stops the interval for the current choice.
 * Polling only runs while the metrics tab is open and the document is
 * visible, so a backgrounded tab stops querying the control plane; each tick
 * skips while a manual load is in flight and refreshes quietly.
 */
function syncMetricRefresh(): void {
  stopMetricRefresh();
  if (
    metricRefreshMs.value === 0 ||
    activeTab.value !== "metrics" ||
    document.hidden
  ) {
    return;
  }
  metricsRefreshTimer = setInterval(() => {
    if (!metricsLoading.value) {
      void loadMetrics({ quiet: true });
    }
  }, metricRefreshMs.value);
}

/**
 * selectMetricRange switches the step and reloads the window. The previous
 * window's points belong to another range, so they are dropped here: a slow or
 * failed read can never render under the new selection's label.
 */
function selectMetricRange(step: MetricStep): void {
  if (step === metricStep.value) {
    return;
  }
  metricStep.value = step;
  metricPoints.value = [];
  metricsLoaded.value = false;
  void loadMetrics();
}

/** isNarrow stacks the two-column descriptions on small screens. */
const isNarrow = useMediaQuery("(max-width: 640px)");

/** descColumns renders descriptions in one column below 640px. */
const descColumns = computed<number>(() => (isNarrow.value ? 1 : 2));

/** initials derives a two-letter avatar from the server name. */
const initials = computed<string>(() => {
  const name = server.value?.name ?? "";
  const letters = name.replace(/[^A-Za-z0-9]/g, "");
  if (letters.length >= 2) {
    return letters.slice(0, 2).toUpperCase();
  }
  if (letters.length === 1) {
    return letters.toUpperCase();
  }
  return "ND";
});

/** summaryLine renders the one-line node summary under the title. */
const summaryLine = computed<string>(() => {
  if (!server.value) {
    return "";
  }
  const parts = [
    `${server.value.ip}:${server.value.port}`,
    server.value.os ?? "Unknown OS",
    server.value.arch ?? "Unknown arch",
    server.value.docker_version ?? "Docker unknown",
  ];
  return parts.join(" · ");
});

/** fallback renders a nullable string field as display text. */
function fallback(value: string | null): string {
  return value ?? "—";
}

/** usageText renders a nullable usage reading as display text.
 *
 * Heartbeat usage arrives as a fraction 0..1 (see toPercent), so the raw
 * reading is normalized before display.
 */
function usageText(value: number | null): string {
  if (value === null || value === undefined) {
    return "—";
  }
  return `${toPercent(value)}%`;
}

/** fetchServer loads one server by route id; 404 surfaces as an error state. */
async function fetchServer(): Promise<void> {
  if (!serverId.value) {
    error.value = "Unknown server.";
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
      message.success(`${server.value.name}: validation passed`);
      return;
    }
    const failed = outcome.checks
      .filter((check) => !check.ok)
      .map((check) => check.name)
      .join(", ");
    message.error(
      outcome.message || `${server.value.name}: failed checks: ${failed}`,
    );
  } catch (err) {
    message.error(describeServerError(err));
  } finally {
    validating.value = false;
  }
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
    message.success(`Deleted ${name}`);
    await router.push({ name: "servers" });
  } catch (err) {
    message.error(describeServerError(err));
  } finally {
    deleting.value = false;
  }
}

watch(serverId, () => {
  activeTab.value = "overview";
  stopMetricRefresh();
  // Invalidate an in-flight read for the previous node: its guarded
  // completion can no longer clear the spinner, so release it here too, or
  // the metrics tab's lazy load would stay blocked until a manual refresh.
  metricsRequestToken += 1;
  metricsLoading.value = false;
  metricStep.value = "1m";
  metricSeriesStep.value = "1m";
  metricPoints.value = [];
  metricsLoaded.value = false;
  metricsError.value = null;
  metricsAvailable.value = true;
  void fetchServer();
});

// The metrics window is read lazily, when the tab is first opened, so an
// overview visit never pulls a series the operator did not ask for.
watch(activeTab, (tab) => {
  if (tab === "metrics" && !metricsLoaded.value && !metricsLoading.value && metricsAvailable.value) {
    void loadMetrics();
  }
});

// Auto-refresh follows the cadence choice as well as tab switches.
watch([metricRefresh, activeTab], syncMetricRefresh);

/** handleVisibilityChange pauses or resumes polling with the document. */
function handleVisibilityChange(): void {
  syncMetricRefresh();
}

onMounted(() => {
  document.addEventListener("visibilitychange", handleVisibilityChange);
  void fetchServer();
});

onBeforeUnmount(() => {
  stopMetricRefresh();
  document.removeEventListener("visibilitychange", handleVisibilityChange);
});
</script>

<template>
  <NSpace vertical :size="16">
    <nav class="breadcrumb" aria-label="Breadcrumb">
      <RouterLink to="/servers">Servers</RouterLink>
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
        <div class="page-head">
          <NAvatar round :size="48">{{ initials }}</NAvatar>
          <div class="page-head__title">
            <NSpace align="center" :size="10">
              <NText strong style="font-size: 20px">{{ server.name }}</NText>
              <ServerStatusTag :status="server.status" size="medium" />
            </NSpace>
            <NText depth="3">{{ summaryLine }}</NText>
          </div>
          <NSpace class="page-head__actions" align="center" :size="8">
            <NButton :loading="validating" @click="handleValidate">
              Validate
            </NButton>
            <RouterLink
              :to="{ name: 'server-containers', params: { id: server.id } }"
              custom
            >
              <template #default="{ navigate }">
                <NButton type="primary" @click="navigate">
                  Open containers
                </NButton>
              </template>
            </RouterLink>
          </NSpace>
        </div>

        <NTabs v-model:value="activeTab" type="line" animated>
          <NTabPane name="overview" tab="Overview">
            <NSpace vertical :size="16" style="margin-top: 16px">
              <NCard title="Node info">
                <NDescriptions :column="descColumns" bordered label-placement="left">
                  <NDescriptionsItem label="Name">
                    {{ server.name }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Address">
                    <span class="mono">{{ server.ip }}:{{ server.port }}</span>
                  </NDescriptionsItem>
                  <NDescriptionsItem label="SSH user">
                    <span class="mono">{{ server.ssh_user }}</span>
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Node ID">
                    <span class="mono">{{ fallback(server.node_id) }}</span>
                  </NDescriptionsItem>
                  <NDescriptionsItem label="OS">
                    {{ fallback(server.os) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Architecture">
                    {{ fallback(server.arch) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Docker">
                    {{ fallback(server.docker_version) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="SSH key">
                    <span class="mono">{{ fallback(server.ssh_key_id) }}</span>
                  </NDescriptionsItem>
                  <NDescriptionsItem label="CPU usage">
                    {{ usageText(server.cpu_usage) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Memory usage">
                    {{ usageText(server.mem_usage) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Disk usage">
                    {{ usageText(server.disk_usage) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Containers">
                    {{ server.container_count ?? "—" }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Last seen">
                    {{ relativeTime(server.last_seen) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Registered">
                    {{ relativeTime(server.created_at) }}
                  </NDescriptionsItem>
                </NDescriptions>
              </NCard>

              <NCard title="Labels">
                <NEmpty description="No labels on this node yet." />
              </NCard>

              <NCard title="Danger zone">
                <NText depth="3">
                  Deleting a node removes it from the control plane only.
                  Containers, volumes and certificates on the machine are kept.
                </NText>
                <div style="margin-top: 12px">
                  <NPopconfirm
                    :positive-button-props="{ type: 'error' }"
                    @positive-click="handleDelete"
                  >
                    <template #trigger>
                      <NButton type="error" ghost :loading="deleting">
                        Delete node
                      </NButton>
                    </template>
                    Delete server "{{ server.name }}"?
                  </NPopconfirm>
                </div>
              </NCard>
            </NSpace>
          </NTabPane>

          <NTabPane name="containers" tab="Containers">
            <NCard style="margin-top: 16px">
              <NEmpty
                description="Container management lives on the containers page."
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
                        Open containers
                      </NButton>
                    </template>
                  </RouterLink>
                </template>
              </NEmpty>
            </NCard>
          </NTabPane>

          <NTabPane name="metrics" tab="Metrics">
            <NSpace vertical :size="16" style="margin-top: 16px">
              <NCard v-if="!metricsAvailable" title="Metrics unavailable">
                <NEmpty description="Server metrics are not enabled on this control plane (FEATURE_METRICS=false)." />
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
                    {{ activeMetricRange.hint }}
                    <template v-if="metricsLoaded && metricSeriesStep !== metricStep">
                      · server returned step {{ metricSeriesStep }}
                    </template>
                  </NText>
                  <NSpace align="center" :size="8">
                    <NText depth="3">Auto-refresh</NText>
                    <NRadioGroup
                      :value="metricRefresh"
                      size="small"
                      aria-label="Metrics auto-refresh"
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
                    Samples are kept 30 days · empty buckets are gaps, not
                    zeros
                  </NText>
                  <NButton
                    size="small"
                    :loading="metricsLoading"
                    @click="void loadMetrics()"
                  >
                    Refresh
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
                      Retry
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
                              ? 'Metrics unavailable.'
                              : metricsLoaded
                                ? 'No samples in this window.'
                                : 'Loading the metrics window…'
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
          </NTabPane>

          <NTabPane name="proxy" tab="Proxy & Traefik">
            <NCard style="margin-top: 16px">
              <NEmpty description="Proxy & Traefik ships in Phase 6." />
            </NCard>
          </NTabPane>

          <NTabPane name="settings" tab="Node settings">
            <NCard title="Node settings" style="margin-top: 16px">
              <NText depth="3" style="display: block; margin-bottom: 12px">
                Editable settings do not exist in the backend yet. Values below
                are read-only.
              </NText>
              <NDescriptions :column="descColumns" bordered label-placement="left">
                <NDescriptionsItem label="Name">
                  {{ server.name }}
                </NDescriptionsItem>
                <NDescriptionsItem label="Address">
                  <span class="mono">{{ server.ip }}:{{ server.port }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="SSH user">
                  <span class="mono">{{ server.ssh_user }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="SSH key">
                  <span class="mono">{{ fallback(server.ssh_key_id) }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Registered">
                  {{ relativeTime(server.created_at) }}
                </NDescriptionsItem>
                <NDescriptionsItem label="Updated">
                  {{ relativeTime(server.updated_at) }}
                </NDescriptionsItem>
              </NDescriptions>
            </NCard>
          </NTabPane>
        </NTabs>
      </template>
    </NSpin>
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

.page-head {
  display: flex;
  align-items: center;
  gap: var(--space-4);
}

.page-head__title {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  flex: 1;
  min-width: 0;
}

.page-head__actions {
  margin-left: auto;
  flex-shrink: 0;
}

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
