
export interface ChannelLogBuffer<T> {
  lines: T[];
  pending: T[];
}

export type ChannelLogBufferStore<T> = Map<string, ChannelLogBuffer<T>>;

export function createChannelLogBufferStore<T>(): ChannelLogBufferStore<T> {
  return new Map<string, ChannelLogBuffer<T>>();
}

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

export function isFrameForChannel(
  frameChannel: string | null,
  activeChannel: string,
): boolean {
  return frameChannel === null || frameChannel === activeChannel;
}

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

export interface ReplayWindow {
  acceptReplay: boolean;
  replayRemaining: number | null;
  replayAccepted: number;
}

export function isStaleResponse(
  capturedGeneration: number,
  currentGeneration: number,
): boolean {
  return capturedGeneration !== currentGeneration;
}

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
