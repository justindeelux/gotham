export type RuntimeHealthState =
  | "healthy"
  | "starting"
  | "degraded"
  | "unavailable"
  | "stopped"
  | "unknown"
  | "disabled";

export interface RuntimeReplicaHealth {
  service?: string;
  replica?: string;
  container_id?: string;
  deployment_id?: string;
  state: string;
  health: string;
  reason?: string;
  coverage: string;
  observed_at?: string;
  last_restart_at?: string;
  last_oom_at?: string;
}

export interface RuntimeHealth {
  state: RuntimeHealthState;
  coverage: string;
  reason?: string;
  observed_at?: string;
  enabled: boolean;
  grace_seconds: number;
  incident_id?: string;
  affected: RuntimeReplicaHealth[];
}

export type RuntimeHealthTag = "success" | "warning" | "error" | "default";

export function runtimeHealthTag(state: RuntimeHealthState): RuntimeHealthTag {
  switch (state) {
    case "healthy":
      return "success";
    case "starting":
      return "warning";
    case "degraded":
    case "unavailable":
      return "error";
    case "stopped":
    case "unknown":
    case "disabled":
    default:
      return "default";
  }
}
