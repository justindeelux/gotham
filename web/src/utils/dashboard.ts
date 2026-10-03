/**
 * Dashboard "Running applications" tile state. A pure, dependency-free
 * decision function so the tile's five states (loading / error / empty /
 * incomplete / ready) are pinned by the ui-truth harness instead of living
 * only in template branches.
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
  /** Ready-state "≥" marker, rendered aria-hidden ahead of the figure. */
  prefix: "" | "≥";
  /** Ready-state running/total numbers behind countText. */
  running: number;
  total: number;
  /** True when some reads failed: the "≥" prefix and the caveat note show. */
  incomplete: boolean;
}

/**
 * applicationTileView decides what the "Running applications" tile shows.
 * Precedence mirrors the page: loading first, then error (never a false
 * "none yet"), then genuinely empty, then ready. A ready tile with failed
 * reads is marked incomplete ("at least N") instead of falsely low.
 */
export function applicationTileView(input: ApplicationTileInput): ApplicationTileView {
  const idle = {
    error: null as string | null,
    countText: "",
    prefix: "" as "" | "≥",
    running: 0,
    total: 0,
    incomplete: false,
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
  const prefix = incomplete ? "≥" : "";
  return {
    ...idle,
    state: "ready",
    incomplete,
    prefix,
    running: input.running,
    total: input.total,
    countText: `${prefix}${input.running}/${input.total}`,
  };
}
