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
