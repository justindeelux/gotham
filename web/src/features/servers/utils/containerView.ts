
import type { Container } from "@/features/servers/api/containers";
import { activeLocale } from "@/shared/i18n/locale";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

export type ContainerFilter = "all" | "running" | "exited";

export function isRunning(state: string): boolean {
  return state.trim().toLowerCase() === "running";
}

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

export function stateLabel(state: string, locale?: string | null): string {
  return state || catalogFor(locale).containers.unknown;
}

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

export function countByFilter(
  containers: Container[],
  filter: ContainerFilter,
): number {
  if (filter === "all") {
    return containers.length;
  }
  return containers.filter((row) => matchesFilter(row, filter)).length;
}
