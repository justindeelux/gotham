
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type { ComputedRef, InjectionKey, Ref } from "vue";

import type { MetricPoint, MetricStep } from "@/features/servers/api/metrics";
import {
  getServerMetrics,
  isMetricsDisabled,
} from "@/features/servers/api/metrics";
import {
  buildMetricCharts,
  isMetricStep,
  metricRanges,
  metricsFailureText,
  refreshMsForChoice,
} from "@/features/servers/utils/serverMetricsView";
import type {
  MetricChart,
  MetricRange,
  MetricRefreshChoice,
} from "@/features/servers/utils/serverMetricsView";
import { activeLocale } from "@/shared/i18n/locale";

export interface ServerMetricsContext {
  metricRanges: MetricRange[];
  metricRefreshOptions: Array<{ label: string; value: MetricRefreshChoice }>;
  metricStep: Ref<MetricStep>;
  metricPoints: Ref<MetricPoint[]>;
  metricsLoading: Ref<boolean>;
  metricsLoaded: Ref<boolean>;
  metricsError: Ref<string | null>;
  metricsAvailable: Ref<boolean>;
  metricRefresh: Ref<MetricRefreshChoice>;
  metricSeriesStep: Ref<MetricStep>;
  activeMetricRange: ComputedRef<MetricRange>;
  appliedMetricRange: ComputedRef<MetricRange>;
  hasMetrics: ComputedRef<boolean>;
  metricCharts: ComputedRef<MetricChart[]>;
  loadMetrics: (_options?: { quiet?: boolean }) => Promise<void>;
  selectMetricRefresh: (_choice: MetricRefreshChoice) => void;
  selectMetricRange: (_step: MetricStep) => void;
}

export const ServerMetricsKey: InjectionKey<ServerMetricsContext> = Symbol("server-metrics");

export function useServerMetrics(
  serverId: Ref<string> | ComputedRef<string>,
  activeTab: Ref<string>,
) {
  const metricStep = ref<MetricStep>("1m");
  const metricPoints = ref<MetricPoint[]>([]);
  const metricsLoading = ref(false);
  const metricsLoaded = ref(false);
  const metricsFailure = ref<unknown>(null);
  const metricsError = computed<string | null>(() =>
    metricsFailureText(metricsFailure.value, activeLocale.value),
  );
  const metricsAvailable = ref(true);

  const metricRefreshOptions: Array<{ label: string; value: MetricRefreshChoice }> =
    [
      { label: "off", value: "off" },
      { label: "15s", value: "15s" },
      { label: "60s", value: "60s" },
    ];
  const metricRefresh = ref<MetricRefreshChoice>("off");

  const metricRefreshMs = computed<number>(() => refreshMsForChoice(metricRefresh.value));

  let metricsRefreshTimer: ReturnType<typeof setInterval> | null = null;

  let metricsRequestsInFlight = 0;

  const metricSeriesStep = ref<MetricStep>("1m");

  let metricsRequestToken = 0;

  const activeMetricRange = computed<MetricRange>(
    () => metricRanges.find((range) => range.step === metricStep.value) ?? metricRanges[0],
  );

  const appliedMetricRange = computed<MetricRange>(
    () =>
      metricRanges.find((range) => range.step === metricSeriesStep.value) ??
      metricRanges[0],
  );

  const hasMetrics = computed<boolean>(() => metricPoints.value.length > 0);

  const metricCharts = computed<MetricChart[]>(() =>
    buildMetricCharts(metricPoints.value, activeLocale.value),
  );

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

    metricsRequestsInFlight += 1;
    if (!quiet) {
      metricsLoading.value = true;
    }
    metricsFailure.value = null;
    try {
      const to = new Date();
      const from = new Date(to.getTime() - range.windowMs);
      const series = await getServerMetrics(requestServerId, from, to, range.step);
      if (!isCurrent()) {
        return;
      }
      metricPoints.value = series.points;
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
        stopMetricRefresh();
        return;
      }
      metricsFailure.value = error;
    } finally {
      metricsRequestsInFlight -= 1;
      if (isCurrent() && !quiet) {
        metricsLoading.value = false;
      }
    }
  }

  function selectMetricRefresh(choice: MetricRefreshChoice): void {
    metricRefresh.value = choice;
  }

  function stopMetricRefresh(): void {
    if (metricsRefreshTimer !== null) {
      clearInterval(metricsRefreshTimer);
      metricsRefreshTimer = null;
    }
  }

  function syncMetricRefresh(): void {
    stopMetricRefresh();
    if (
      metricRefreshMs.value === 0 ||
      !metricsAvailable.value ||
      activeTab.value !== "metrics" ||
      document.hidden
    ) {
      return;
    }
    metricsRefreshTimer = setInterval(() => {
      if (metricsRequestsInFlight === 0) {
        void loadMetrics({ quiet: true });
      }
    }, metricRefreshMs.value);
  }

  function selectMetricRange(step: MetricStep): void {
    if (step === metricStep.value) {
      return;
    }
    metricStep.value = step;
    metricPoints.value = [];
    metricsLoaded.value = false;
    void loadMetrics();
  }

  function resetForServer(): void {
    metricsRequestToken += 1;
    metricsLoading.value = false;
    metricStep.value = "1m";
    metricSeriesStep.value = "1m";
    metricPoints.value = [];
    metricsLoaded.value = false;
    metricsFailure.value = null;
    metricsAvailable.value = true;
    stopMetricRefresh();
  }

  watch(serverId, resetForServer);

  watch(activeTab, (tab) => {
    if (tab === "metrics" && !metricsLoaded.value && !metricsLoading.value && metricsAvailable.value) {
      void loadMetrics();
    }
  });

  watch([metricRefresh, activeTab], syncMetricRefresh);

  function handleVisibilityChange(): void {
    syncMetricRefresh();
  }

  if (typeof document !== "undefined") {
    document.addEventListener("visibilitychange", handleVisibilityChange);
  }

  onBeforeUnmount(() => {
    stopMetricRefresh();
    if (typeof document !== "undefined") {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    }
  });

  const context: ServerMetricsContext = {
    metricRanges,
    metricRefreshOptions,
    metricStep,
    metricPoints,
    metricsLoading,
    metricsLoaded,
    metricsError,
    metricsAvailable,
    metricRefresh,
    metricSeriesStep,
    activeMetricRange,
    appliedMetricRange,
    hasMetrics,
    metricCharts,
    loadMetrics,
    selectMetricRefresh,
    selectMetricRange,
  };

  return { context, resetForServer };
}
