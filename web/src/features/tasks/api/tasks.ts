/**
 * Background-task realtime contract (JUS-91).
 *
 * The control plane publishes task lifecycle events on `tasks:{teamID}`
 * (`internal/taskevents`): live frames `{ channel, type: "task", data }`
 * where `data` is a JSON TaskEvent, plus a `task_snapshot` end marker
 * closing the running-task replay sent after every (re)subscribe.
 */

/** Lifecycle state carried by a task event. */
export type TaskStatus = "queued" | "running" | "succeeded" | "failed";

/** One background-task lifecycle event. */
export interface TaskEvent {
  taskId: string;
  kind: string;
  name: string;
  status: TaskStatus;
  step: string;
  progress: number;
  appId: string;
  deploymentId: string;
  serverId: string;
  projectId: string;
  environmentId: string;
  teamId: string;
  error: string;
}

/** taskChannel returns the realtime room of one team's task events. */
export function taskChannel(teamId: string): string {
  return `tasks:${teamId}`;
}

/** isTerminalStatus reports whether a task reached its final state. */
export function isTerminalStatus(status: TaskStatus): boolean {
  return status === "succeeded" || status === "failed";
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

/**
 * parseTaskFrame decodes one realtime frame into a TaskEvent, or null when
 * the frame is not a task event (log frames, acks, the snapshot marker).
 */
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
  return {
    taskId,
    kind: asString(event.kind),
    name: asString(event.name),
    status,
    step: asString(event.step),
    progress,
    appId: asString(event.app_id),
    deploymentId: asString(event.deployment_id),
    serverId: asString(event.server_id),
    projectId: asString(event.project_id),
    environmentId: asString(event.environment_id),
    teamId: asString(event.team_id),
    error: asString(event.error),
  };
}
