import { http } from "@/shared/api/http";
import { activeLocale } from "@/shared/i18n/locale";
import { isApiError, stripErrorPrefix } from "./servers";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

export type MetricStep = "1m" | "1h" | "1d";

export interface MetricPoint {
  bucket: string;
  cpu_usage: number;
  mem_usage: number;
  disk_usage: number;
  net_rx_bps: number;
  net_tx_bps: number;
  disk_read_bps: number;
  disk_write_bps: number;
  container_count: number;
}

export interface MetricSeries {
  step: MetricStep;
  points: MetricPoint[];
}

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

export function isMetricsDisabled(error: unknown): boolean {
  return isApiError(error) && error.status === 404;
}

export function describeMetricsError(error: unknown, locale?: string | null): string {
  const errors = catalogFor(locale).errors;
  if (isApiError(error)) {
    if (error.status === 401) {
      return errors.sessionExpired;
    }
    if (error.status === 404) {
      return errors.metricsDisabled;
    }
    if (error.status === 400) {
      return (
      stripErrorPrefix(error.message) ||
      errors.metricsRange
    );
    }
    return stripErrorPrefix(error.message) || errors.requestFailed;
  }
  if (error instanceof Error) {
    return stripErrorPrefix(error.message) || errors.unexpected;
  }
  return errors.unexpected;
}
