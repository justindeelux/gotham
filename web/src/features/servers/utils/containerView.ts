// Pure view helpers for the containers page (extracted from ContainersPage).

import type { Container } from "@/features/servers/api/containers";
import { activeLocale } from "@/shared/i18n/locale";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

/** catalogFor selects the servers display dictionary for one locale. */
function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

/** Status chips mirroring the server-detail mockup toolbar. */
export type ContainerFilter = "all" | "running" | "exited";

/** isRunning reports whether a container is in the running state. */
export function isRunning(state: string): boolean {
  return state.trim().toLowerCase() === "running";
}

/** matchesFilter applies the active status chip to one container. */
export function matchesFilter(row: Container, filter: ContainerFilter): boolean {
  switch (filter) {
    case "running":
      return isRunning(row.state);
    case "exited":
      return !isRunning(row.state);
    case "all":
    default:
      return true;
  }
}

/** stateLabel renders the raw Docker state, or the unknown fallback. */
export function stateLabel(state: string, locale?: string | null): string {
  return state || catalogFor(locale).containers.unknown;
}

/** Tag type for a raw Docker lifecycle state. */
export function stateTagType(state: string): "default" | "success" | "warning" | "error" {
  switch (state.trim().toLowerCase()) {
    case "running":
      return "success";
    case "restarting":
    case "paused":
    case "created":
      return "warning";
    case "exited":
    case "dead":
      return "error";
    default:
      return "default";
  }
}

/** countByFilter renders live chip counts; never invented. */
export function countByFilter(
  containers: Container[],
  filter: ContainerFilter,
): number {
  if (filter === "all") {
    return containers.length;
  }
  return containers.filter((row) => matchesFilter(row, filter)).length;
}
