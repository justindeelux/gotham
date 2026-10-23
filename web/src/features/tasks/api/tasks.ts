
export type TaskStatus = "queued" | "running" | "succeeded" | "failed";

export interface TaskEvent {
  taskId: string;
  kind: string;
  name: string;
  status: TaskStatus;
  step: string;
  progress: number;
  seq: number;
  appId: string;
  deploymentId: string;
  serverId: string;
  projectId: string;
  environmentId: string;
  teamId: string;
  error: string;
}

export function taskChannel(teamId: string): string {
  return `tasks:${teamId}`;
}

export function isTerminalStatus(status: TaskStatus): boolean {
  return status === "succeeded" || status === "failed";
}

export function isTaskSnapshotMarker(raw: string): boolean {
  try {
    const parsed: unknown = JSON.parse(raw);
    return (
      isRecord(parsed) &&
      parsed.type === "task_snapshot" &&
      typeof parsed.channel === "string"
    );
  } catch {
    return false;
  }
}

interface TaskFrame {
  channel?: unknown;
  type?: unknown;
  data?: unknown;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function asStatus(value: unknown): TaskStatus | null {
  return value === "queued" ||
    value === "running" ||
    value === "succeeded" ||
    value === "failed"
    ? value
    : null;
}

export function parseTaskFrame(raw: string): TaskEvent | null {
  let frame: TaskFrame;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!isRecord(parsed)) {
      return null;
    }
    frame = parsed;
  } catch {
    return null;
  }
  if (frame.type !== "task" || typeof frame.data !== "string") {
    return null;
  }
  let event: unknown;
  try {
    event = JSON.parse(frame.data);
  } catch {
    return null;
  }
  if (!isRecord(event)) {
    return null;
  }
  const status = asStatus(event.status);
  const taskId = asString(event.task_id);
  if (status === null || taskId === "") {
    return null;
  }
  const progress =
    typeof event.progress === "number" && Number.isFinite(event.progress)
      ? Math.min(100, Math.max(0, Math.round(event.progress)))
      : 0;
  const seq =
    typeof event.seq === "number" && Number.isFinite(event.seq) && event.seq > 0
      ? Math.floor(event.seq)
      : 0;
  return {
    taskId,
    kind: asString(event.kind),
    name: asString(event.name),
    status,
    step: asString(event.step),
    progress,
    seq,
    appId: asString(event.app_id),
    deploymentId: asString(event.deployment_id),
    serverId: asString(event.server_id),
    projectId: asString(event.project_id),
    environmentId: asString(event.environment_id),
    teamId: asString(event.team_id),
    error: asString(event.error),
  };
}
