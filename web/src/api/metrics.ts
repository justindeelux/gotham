import { http } from "./http";
import { isApiError, stripErrorPrefix } from "./servers";

/**
 * Typed client for the server metrics range route served by
 * `internal/server/servers_routes.go`:
 *
 *   GET /servers/{id}/metrics?from&to&step
 *
 * `from`/`to` are RFC 3339 timestamps and `step` is one of `1m`, `1h`, `1d`
 * (see `ParseMetricsQuery` in internal/servers/metrics.go, which also caps the
 * window per step). The route exists only while FEATURE_METRICS is on; disabled
 * it answers 404 and the charts surface is hidden rather than rendered as an
 * error.
 */

/** Aggregation bucket accepted by the metrics route. */
export type MetricStep = "1m" | "1h" | "1d";

/**
 * One aggregated bucket. Buckets without samples are omitted by the backend,
 * so a jump between two `bucket` values is a real gap in the series — the
 * chart must not interpolate a zero through it.
 */
export interface MetricPoint {
  bucket: string;
  /** CPU usage as a fraction 0..1 (see toPercent in utils/format). */
  cpu_usage: number;
  mem_usage: number;
  disk_usage: number;
  net_rx_bps: number;
  net_tx_bps: number;
  disk_read_bps: number;
  disk_write_bps: number;
  container_count: number;
}

/** One metrics range answer (`metricsEnvelope` in servers_routes.go). */
export interface MetricSeries {
  step: MetricStep;
  points: MetricPoint[];
}

/** getServerMetrics reads one server's aggregated series for [from, to). */
export async function getServerMetrics(
  serverId: string,
  from: Date,
  to: Date,
  step: MetricStep,
): Promise<MetricSeries> {
  const response = await http.get<MetricSeries>(`/servers/${serverId}/metrics`, {
    params: {
      from: from.toISOString(),
      to: to.toISOString(),
      step,
    },
  });
  return {
    step: response.data.step ?? step,
    points: response.data.points ?? [],
  };
}

/** isMetricsDisabled reports whether an error is the feature-flag 404. */
export function isMetricsDisabled(error: unknown): boolean {
  return isApiError(error) && error.status === 404;
}

/** describeMetricsError maps a thrown error to a user-facing message. */
export function describeMetricsError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 401) {
      return "Your session expired. Please sign in again.";
    }
    if (error.status === 404) {
      return "Server metrics are not enabled on this control plane (FEATURE_METRICS=false).";
    }
    if (error.status === 400) {
      return (
      stripErrorPrefix(error.message) ||
      "Invalid metrics range. Pick another range and retry."
    );
    }
    return stripErrorPrefix(error.message) || "Request failed";
  }
  if (error instanceof Error) {
    return stripErrorPrefix(error.message) || "Something went wrong. Please try again.";
  }
  return "Something went wrong. Please try again.";
}
