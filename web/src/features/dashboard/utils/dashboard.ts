/**
 * Dashboard "Running applications" tile state. Pure, dependency-free
 * decisions so the tile's four states (loading / error / empty / ready) are
 * pinned by the ui-truth harness instead of living only in template
 * branches. The ready figure and its incompleteness hint are owned here as
 * rendered text: the template binds `tile.countText` / `tile.hint` directly
 * and formats nothing itself.
 */

/** Input to {@link applicationTileView}. */
export interface ApplicationTileInput {
  loading: boolean;
  error: string | null;
  total: number;
  running: number;
  /** Per-application reads that failed; >0 marks the figure incomplete. */
  failedReads: number;
}

/**
 * Page-owned values mapped into {@link ApplicationTileInput}. The page
 * passes its live refs through {@link buildApplicationTileInput} so the
 * mapping (and any hardcoded override of it) is pinned by the harness.
 */
export interface ApplicationTileSource {
  loading: boolean;
  error: string | null;
  total: number;
  running: number;
  failedReads: number;
}

/**
 * buildApplicationTileInput maps the page's live tile values into the pure
 * decision input. Every field passes through untouched: an `error: null` or
 * `failedReads: 0` override at the call site changes the result and fails
 * the mapping check.
 */
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

/** Render state of the "Running applications" tile. */
export type ApplicationTileState =
  | "loading"
  | "error"
  | "empty"
  | "ready";

/** Render decision for the "Running applications" tile. */
export interface ApplicationTileView {
  state: ApplicationTileState;
  /** Set only in the error state; the page renders it with a retry. */
  error: string | null;
  /** Ready-state figure text, e.g. "1/2" or "≥1/2" when incomplete. */
  countText: string;
  /** Ready-state caveat, e.g. "Some states could not be read"; "" when exact. */
  hint: string;
}

import { activeLocale } from "@/shared/i18n/locale";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

/** catalogFor selects the dashboard display dictionary for one locale. */
function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

/** Hint shown under a ready tile whose figure is a lower bound. */
export const incompleteTileHint = "Some states could not be read";

/**
 * applicationTileView decides what the "Running applications" tile shows.
 * Precedence mirrors the page: loading first, then error (never a false
 * "none yet"), then genuinely empty, then ready. A ready tile with failed
 * reads keeps the "at least N" figure and hint instead of a falsely low one.
 * The incomplete hint follows the display locale; the figure itself is
 * locale-independent counts.
 */
export function applicationTileView(
  input: ApplicationTileInput,
  locale?: string | null,
): ApplicationTileView {
  const idle = {
    error: null as string | null,
    countText: "",
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
    hint: incomplete ? catalogFor(locale).tiles.incompleteHint : "",
  };
}
