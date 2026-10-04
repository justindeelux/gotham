/**
 * Per-channel log buffers for the realtime log viewer (B2-2 / C4-4).
 *
 * A viewer keeps its rendered and paused lines in one place. When its channel
 * changes (a different container, or DeployLogs pointing at another
 * deployment) the lines must be scoped to that channel id instead of carried
 * into the new title, and frames already in flight for the channel just left
 * must not render. This module owns those two decisions; it is framework-free
 * so the Node check (`scripts/mock-ws-check.mjs`) can exercise it directly.
 */

/** Rendered and queued lines for one channel. */
export interface ChannelLogBuffer<T> {
  lines: T[];
  pending: T[];
}

/** A store of per-channel buffers keyed by channel id. */
export type ChannelLogBufferStore<T> = Map<string, ChannelLogBuffer<T>>;

/** createChannelLogBufferStore returns an empty store. */
export function createChannelLogBufferStore<T>(): ChannelLogBufferStore<T> {
  return new Map<string, ChannelLogBuffer<T>>();
}

/**
 * activateChannel saves the buffer currently on screen under `previous` and
 * returns the buffer to render for `next`, reusing it when that channel was
 * seen before and starting empty otherwise. This is what stops two streams
 * from being concatenated under one title.
 */
export function activateChannel<T>(
  store: ChannelLogBufferStore<T>,
  previous: string,
  next: string,
  current: ChannelLogBuffer<T>,
): ChannelLogBuffer<T> {
  if (previous) {
    store.set(previous, current);
  }
  return store.get(next) ?? { lines: [], pending: [] };
}

/** isFrameForChannel reports whether a frame belongs to the active channel. */
export function isFrameForChannel(
  frameChannel: string | null,
  activeChannel: string,
): boolean {
  return frameChannel === null || frameChannel === activeChannel;
}

/**
 * flushPendingLines appends a channel's queued (paused) lines to its rendered
 * lines in order, then drops the oldest past `maxLines`. It mutates both arrays
 * in place so a per-channel buffer keeps its array identity. Exposed for the
 * viewer's resume and switch-restore paths so a restored backlog is never
 * stranded behind later live lines (U2).
 */
export function flushPendingLines<T>(
  buffer: ChannelLogBuffer<T>,
  maxLines: number,
): void {
  if (buffer.pending.length === 0) {
    return;
  }
  buffer.lines.push(...buffer.pending);
  buffer.pending.length = 0;
  if (buffer.lines.length > maxLines) {
    buffer.lines.splice(0, buffer.lines.length - maxLines);
  }
}

/** Mutable replay-window state for an in-flight start request. */
export interface ReplayWindow {
  acceptReplay: boolean;
  replayRemaining: number | null;
  replayAccepted: number;
}

/**
 * isStaleResponse reports whether a start response captured at
 * `capturedGeneration` no longer belongs to the viewer's current channel
 * generation, i.e. the viewer switched channel while the request was in flight
 * (U1). The viewer calls this (via the two apply helpers) from both the success
 * and failure callbacks so a stale response can neither rewrite the new
 * channel's replay window nor append a notice to its buffer.
 */
export function isStaleResponse(
  capturedGeneration: number,
  currentGeneration: number,
): boolean {
  return capturedGeneration !== currentGeneration;
}

/**
 * applyStartSuccess folds a successful start response's replay count into the
 * window, ignoring a response captured for a channel the viewer has left.
 * Returns whether the response was applied.
 */
export function applyStartSuccess(
  window: ReplayWindow,
  capturedGeneration: number,
  currentGeneration: number,
  replayCount: number,
): boolean {
  if (isStaleResponse(capturedGeneration, currentGeneration)) {
    return false;
  }
  window.replayRemaining = replayCount;
  if (replayCount === 0 || window.replayAccepted >= replayCount) {
    window.acceptReplay = false;
  }
  return true;
}

/**
 * applyStartFailure folds a failed start response into the viewer's buffer as a
 * notice, queued while paused and capped at `maxLines`, ignoring a response
 * captured for a channel the viewer has left. Returns whether it was applied.
 */
export function applyStartFailure<T>(
  window: ReplayWindow,
  capturedGeneration: number,
  currentGeneration: number,
  buffer: ChannelLogBuffer<T>,
  paused: boolean,
  notice: T,
  maxLines: number,
): boolean {
  if (isStaleResponse(capturedGeneration, currentGeneration)) {
    return false;
  }
  window.acceptReplay = false;
  const target = paused ? buffer.pending : buffer.lines;
  target.push(notice);
  if (target.length > maxLines) {
    target.splice(0, target.length - maxLines);
  }
  return true;
}
