import { http } from "@/shared/api/http";

export interface Rule {
  enabled: boolean;
  warning: number;
  clear: number;
  confirm_seconds: number;
  clear_seconds: number;
}

export interface FilesystemRule {
  enabled: boolean;
  warning: number;
  critical: number;
  downgrade: number;
  clear: number;
  confirm_seconds: number;
  critical_seconds: number;
  downgrade_seconds: number;
  clear_seconds: number;
}

export interface MonitoringRules {
  cpu: Rule;
  memory: Rule;
  space: FilesystemRule;
  inode: FilesystemRule;
}

export interface MonitoringSettings {
  node_id?: string;
  rules: MonitoringRules;
  effective: MonitoringRules;
  defaults: MonitoringRules;
  source: "default" | "instance" | "node";
  has_override: boolean;
  updated_at?: string;
}

export interface ActivePressure {
  detector: "host_cpu" | "host_memory" | "filesystem_space" | "filesystem_inode";
  severity: "warning" | "critical";
  lifecycle: "pending" | "firing" | "resolved";
  resource_name: string;
  evidence: string;
  first_observed_at: string;
  confirmed_at?: string;
}

export interface FilesystemStatus {
  path: string;
  purpose: string;
  status: string;
  total_bytes: number;
  available_bytes: number;
  used_percent: number;
  inode_status: string;
  total_inodes: number;
  free_inodes: number;
  inode_used_percent: number;
}

export interface NodeStatus {
  node_id: string;
  observed_at?: string;
  coverage: "fresh" | "stale" | "unknown";
  active: ActivePressure[];
  filesystems: FilesystemStatus[];
}

interface SettingsEnvelope {
  settings: MonitoringSettings;
}

interface StatusEnvelope {
  status: NodeStatus;
}

export async function getMonitoringSettings(nodeId?: string): Promise<MonitoringSettings> {
  const response = await http.get<SettingsEnvelope>("/monitoring/settings", {
    params: nodeId ? { node_id: nodeId } : {},
  });
  return response.data.settings;
}

export async function saveMonitoringSettings(
  rules: MonitoringRules,
  nodeId?: string,
): Promise<MonitoringSettings> {
  const response = await http.put<SettingsEnvelope>(
    "/monitoring/settings",
    nodeId ? { node_id: nodeId, rules } : { rules },
  );
  return response.data.settings;
}

export async function resetMonitoringSettings(nodeId?: string): Promise<MonitoringSettings> {
  const response = await http.delete<SettingsEnvelope>("/monitoring/settings", {
    params: nodeId ? { node_id: nodeId } : {},
  });
  return response.data.settings;
}

export async function getNodeStatus(nodeId: string): Promise<NodeStatus> {
  const response = await http.get<StatusEnvelope>(`/monitoring/nodes/${nodeId}/status`);
  return response.data.status;
}

export function describeMonitoringError(error: unknown): string {
  if (typeof error === "object" && error !== null && "message" in error) {
    const message = (error as { message?: unknown }).message;
    if (typeof message === "string" && message.trim() !== "") {
      return message;
    }
  }
  return error instanceof Error ? error.message : String(error);
}
