
export interface ApplicationTileInput {
  loading: boolean;
  error: string | null;
  total: number;
  running: number;
  failedReads: number;
}

export interface ApplicationTileSource {
  loading: boolean;
  error: string | null;
  total: number;
  running: number;
  failedReads: number;
}

export function buildApplicationTileInput(
  source: ApplicationTileSource,
): ApplicationTileInput {
  return {
    loading: source.loading,
    error: source.error,
    total: source.total,
    running: source.running,
    failedReads: source.failedReads,
  };
}

export type ApplicationTileState =
  | "loading"
  | "error"
  | "empty"
  | "ready";

export interface ApplicationTileView {
  state: ApplicationTileState;
  error: string | null;
  countText: string;
  runningText: string;
  hint: string;
}

import { activeLocale } from "@/shared/i18n/locale";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

export const incompleteTileHint = "Some states could not be read";

export function applicationTileView(
  input: ApplicationTileInput,
  locale?: string | null,
): ApplicationTileView {
  const idle = {
    error: null as string | null,
    countText: "",
    runningText: "",
    hint: "",
  };
  if (input.loading) {
    return { ...idle, state: "loading" };
  }
  if (input.error) {
    return { ...idle, state: "error", error: input.error };
  }
  if (input.total <= 0) {
    return { ...idle, state: "empty" };
  }
  const incomplete = input.failedReads > 0;
  return {
    ...idle,
    state: "ready",
    countText: `${incomplete ? "≥" : ""}${input.running}/${input.total}`,
    runningText: `${incomplete ? "≥" : ""}${input.running}`,
    hint: incomplete ? catalogFor(locale).tiles.incompleteHint : "",
  };
}
