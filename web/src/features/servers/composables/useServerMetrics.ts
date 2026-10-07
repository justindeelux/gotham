// Metrics state for the server detail page (extracted from ServerDetailPage).
//
// Owns the metrics window, its auto-refresh and the chart projections. The
// page owns the server header and tabs; the metrics tab injects this context
// instead of receiving a dozen props.

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
  /**
   * metricsFailure keeps the raw last window failure. The `metricsError`
   * display derives from it in the current locale, so a language switch
   * re-renders a retained alert without reloading the window or touching
   * the auto-refresh cadence.
   */
  const metricsFailure = ref<unknown>(null);
  /** metricsError renders the retained failure, or null while healthy. */
  const metricsError = computed<string | null>(() =>
    metricsFailureText(metricsFailure.value, activeLocale.value),
  );
  /** False once the API answers the FEATURE_METRICS 404: the charts are hidden. */
  const metricsAvailable = ref(true);

  const metricRefreshOptions: Array<{ label: string; value: MetricRefreshChoice }> =
    [
      { label: "off", value: "off" },
      { label: "15s", value: "15s" },
      { label: "60s", value: "60s" },
    ];
  const metricRefresh = ref<MetricRefreshChoice>("off");

  /** Cadence of the auto-refresh in milliseconds; 0 keeps it off. */
  const metricRefreshMs = computed<number>(() => refreshMsForChoice(metricRefresh.value));

  /** Interval handle of the auto-refresh; null while it is off. */
  let metricsRefreshTimer: ReturnType<typeof setInterval> | null = null;

  /**
   * Number of metrics fetches in flight (manual or quiet). A tick starts only
   * when the count is zero: the newest request must not clear the guard for an
   * older one still pending, or a later tick could overlap it.
   */
  let metricsRequestsInFlight = 0;

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
   * metricCharts maps the window's points onto the four real charts. Nothing is
   * synthesized: every series is a projection of the returned `points`. Chart
   * titles follow the display locale; thresholds and values are untouched.
   */
  const metricCharts = computed<MetricChart[]>(() =>
    buildMetricCharts(metricPoints.value, activeLocale.value),
  );

  /**
   * loadMetrics reads the active step's window. Completion is guarded by both a
   * request generation and the server ID, so a late response for another range
   * or another node can never overwrite the current one. A feature-flag 404
   * hides the charts instead of rendering an error; any other failure surfaces
   * an explicit error with a retry. A quiet call (auto-refresh) never toggles
   * the spinner: a background tick must not flash the loading state. Either
   * kind of fetch raises the in-flight count, so a tick can never overlap one
   * already running.
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
        // The control disappears with the charts, so the operator cannot turn
        // the cadence off: stop polling here instead of leaving silent 404s.
        stopMetricRefresh();
        return;
      }
      metricsFailure.value = error;
    } finally {
      // Every request releases its own count. Only the newest one owns the
      // spinner; an obsolete one must not clear a loading state the current
      // request still needs.
      metricsRequestsInFlight -= 1;
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
   * Polling only runs while the metrics feature is available, the metrics tab
   * is open and the document is visible, so a disabled feature or a
   * backgrounded tab stops querying the control plane; each tick skips while
   * any fetch is already in flight and refreshes quietly.
   */
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

  /** resetForServer drops the window when the route moves to another node. */
  function resetForServer(): void {
    // Invalidate an in-flight read for the previous node: its guarded
    // completion can no longer clear the spinner, so release it here too, or
    // the metrics tab's lazy load would stay blocked until a manual refresh.
    // The in-flight count is owned by each request's finally and needs no reset.
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

  // Visibility polling is armed here so the metrics tab works even when the
  // page never mounts the listener itself; the page owns no metrics timers.
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
