import { http } from "@/shared/api/http";

export type ResourceHistoryRange = "1h" | "24h" | "7d" | "30d";

export interface ResourceHistoryPoint {
  bucket: string;
  cpu_cores: number | null;
  mem_working_set_bytes: number | null;
  mem_limit_bytes: number;
  mem_unlimited: boolean;
  net_rx_bps: number | null;
  net_tx_bps: number | null;
  disk_read_bps: number | null;
  disk_write_bps: number | null;
  latest_at: string;
  replicas: number;
  expected_replicas: number;
  coverage: "complete" | "partial";
}

export interface ResourceHistorySeries {
  service: string;
  deployment_id: string;
  points: ResourceHistoryPoint[];
}

export interface ResourceHistoryMarker {
  at: string;
  deployment_id: string;
  service: string;
}

export interface ResourceHistory {
  range: ResourceHistoryRange;
  step: string;
  from: string;
  to: string;
  capability: "available" | "unavailable";
  coverage: "complete" | "partial" | "unknown";
  series: ResourceHistorySeries[];
  markers: ResourceHistoryMarker[];
}

export type ResourceType = "application" | "database";

export async function getResourceHistory(
  resourceType: ResourceType,
  resourceId: string,
  range: ResourceHistoryRange,
  service = "",
): Promise<ResourceHistory> {
  const path =
    resourceType === "application"
      ? `/applications/${resourceId}/metrics`
      : `/databases/${resourceId}/metrics`;
  const { data } = await http.get<ResourceHistory>(path, {
    params: service ? { range, service } : { range },
  });
  return data;
}
