import type { Deployment } from "@/features/applications/api/applications";

/** durationText renders started→finished (or started→now) as a short span. */
export function durationText(deployment: Deployment): string {
  if (!deployment.started_at) {
    return "—";
  }
  const start = new Date(deployment.started_at).getTime();
  const end = deployment.finished_at ? new Date(deployment.finished_at).getTime() : Date.now();
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) {
    return "—";
  }
  const seconds = Math.round((end - start) / 1000);
  if (seconds < 60) {
    return `${seconds}s`;
  }
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return `${minutes}m${seconds % 60}s`;
  }
  return `${Math.floor(minutes / 60)}h${minutes % 60}m`;
}
