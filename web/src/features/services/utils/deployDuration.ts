import type { ServiceDeploy } from "@/features/services/api/services";

/** durationText renders created→finished (or created→now) as a short span. */
export function durationText(deploy: ServiceDeploy): string {
  const start = new Date(deploy.created_at).getTime();
  const end = deploy.finished_at
    ? new Date(deploy.finished_at).getTime()
    : Date.now();
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
