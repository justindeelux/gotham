<script setup lang="ts">
import { NButton } from "naive-ui";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";

import {
  describeContainerError,
  startContainerLogStream,
} from "../api/containers";
import { refreshSession } from "../api/http";
import { getAccessToken } from "../api/token";
import {
  activateChannel,
  applyStartFailure,
  applyStartSuccess,
  createChannelLogBufferStore,
  flushPendingLines,
  isFrameForChannel,
} from "../composables/logChannelBuffers";
import type {
  ChannelLogBuffer,
  ReplayWindow,
} from "../composables/logChannelBuffers";
import type { WebSocketMessage } from "../composables/useWebSocket";
import { useWebSocket } from "../composables/useWebSocket";

/**
 * Monospace log terminal for the realtime drawer.
 *
 * Renders raw frames pushed by the realtime hub; it never fabricates log
 * content. Auto-scroll follows the tail until the reader scrolls up or flips
 * the follow toggle; pause freezes the view and queues incoming lines.
 */

interface Props {
  /** Server whose agent sources the log stream. */
  serverId: string;
  /** Container (or deploy container) being streamed. */
  containerId: string;
  /** Drawer heading; the header renders only when set. */
  title?: string;
  /** Drawer sub-heading. */
  subtitle?: string;
  /** Explicit channel override; defaults to `logs:{serverId}:{containerId}`. */
  channel?: string;
  /**
   * When true, ask the control plane to bridge the agent log stream on mount.
   * Only valid for raw container logs (`logs:{serverId}:{containerId}`); the
   * deploy-log wrapper leaves it off because its channel is already published.
   */
  autoStartStream?: boolean;
  /** Realtime endpoint path. */
  wsPath?: string;
  /** Maximum rendered lines before the oldest are dropped. */
  maxLines?: number;
}

const props = withDefaults(defineProps<Props>(), {
  title: "",
  subtitle: "",
  channel: "",
  autoStartStream: false,
  wsPath: "/api/v1/ws",
  maxLines: 2000,
});

/** One rendered log line. */
interface LogLine {
  id: number;
  ts: string;
  text: string;
  kind: "line" | "notice";
}

const lines = ref<LogLine[]>([]);
const pending = ref<LogLine[]>([]);
const isPaused = ref(false);
const isFollowing = ref(true);
const logBody = ref<HTMLElement | null>(null);
/** Rendered/paused lines keyed by channel, so a switch cannot mix streams. */
const channelBuffers = createChannelLogBufferStore<LogLine>();

let lineId = 0;
let scrollQueued = false;
/**
 * Replay-window state, decided once per start (round-3/4/5 U1):
 *  - `acceptReplay`: whether tagged history should render at all (the viewer
 *    was empty when its start was requested).
 *  - `replayRemaining`: the frame count the start response reported, or null
 *    until it arrives (tagged frames may beat the HTTP response).
 *  - `replayAccepted`: how many tagged frames have rendered in this window.
 * A live frame never closes the window; the count, the server's `replay_end`
 * marker, or the timeout do.
 */
const replayWindow: ReplayWindow = {
  acceptReplay: false,
  replayRemaining: null,
  replayAccepted: 0,
};
let replayTimer: ReturnType<typeof setTimeout> | null = null;
/**
 * Bumped on every channel switch so an in-flight start response for a channel
 * the viewer has left cannot mutate the new channel's replay state (U1).
 */
let channelGeneration = 0;

/** Fallback window for a replay whose counted frames never arrive, in ms. */
const replayWindowMs = 5_000;

/** clearReplayTimer cancels the fallback replay-window timeout. */
function clearReplayTimer(): void {
  if (replayTimer !== null) {
    clearTimeout(replayTimer);
    replayTimer = null;
  }
}

/** resetReplayState drops any in-flight replay window for the previous channel. */
function resetReplayState(): void {
  clearReplayTimer();
  replayWindow.acceptReplay = false;
  replayWindow.replayRemaining = null;
  replayWindow.replayAccepted = 0;
}

const channelName = computed<string>(
  () => props.channel || `logs:${props.serverId}:${props.containerId}`,
);

const {
  status: streamStatus,
  connect: connectStream,
  close: closeStream,
  subscribe: subscribeChannel,
  unsubscribe: unsubscribeChannel,
  clearBuffer: clearStreamBuffer,
} = useWebSocket({
  url: props.wsPath,
  token: getAccessToken,
  onMessage: handleMessage,
  // On repeated reconnect failures, refresh the session once so the token
  // getter can pick up a rotated token after the 15-min TTL (B2-3/U4).
  onReconnectFailed: refreshSession,
});

const statusLabel = computed<string>(() => {
  if (isPaused.value) {
    return "Paused";
  }
  switch (streamStatus.value) {
    case "open":
      return "Streaming";
    case "connecting":
      return "Connecting";
    case "reconnecting":
      return "Reconnecting";
    case "error":
      return "Offline";
    case "closed":
      return "Closed";
    default:
      return "Idle";
  }
});

const statusClasses = computed<Record<string, boolean>>(() => ({
  "is-paused": isPaused.value || streamStatus.value === "reconnecting",
  "is-offline":
    streamStatus.value === "error" ||
    streamStatus.value === "closed" ||
    streamStatus.value === "idle",
}));

/** handleMessage converts a hub frame into zero or more rendered lines. */
function handleMessage(message: WebSocketMessage): void {
  // Frames already in flight for the channel just left must not render under
  // the new channel's title (B2-2 / C4-4).
  if (!isFrameForChannel(message.channel, channelName.value)) {
    return;
  }

  // Start the agent stream only once the server has acknowledged this
  // channel's subscription: the hub room must have a member before the agent's
  // historical tail is published, or the first lines are lost (U2).
  if (
    message.payload?.type === "subscribed" &&
    message.channel === channelName.value
  ) {
    requestStreamStart();
    return;
  }

  // A denied subscription (authorization or the per-connection cap) never
  // joins the room, so the drawer would otherwise wait forever: surface the
  // server's reason as a notice (round-2 U4).
  if (message.payload?.type === "denied") {
    const reason =
      typeof message.payload.data === "string" ? message.payload.data : "";
    appendLine({
      id: ++lineId,
      ts: formatTimestamp(null, message.receivedAt),
      text: reason ? `Subscription denied: ${reason}` : "Subscription denied",
      kind: "notice",
    });
    return;
  }

  // A transport recovery notice closes out the interruption notice.
  if (message.payload?.type === "resumed") {
    appendLine({
      id: ++lineId,
      ts: formatTimestamp(null, message.receivedAt),
      text: "Log stream resumed",
      kind: "notice",
    });
    return;
  }

  // End of a replay batch: stop accepting tagged history so a later viewer's
  // replay is never rendered on top of this viewer's lines (round-4/5 U1).
  if (message.payload?.type === "replay_end") {
    replayWindow.acceptReplay = false;
    clearReplayTimer();
    return;
  }

  if (message.kind === "notice") {
    appendLine({
      id: ++lineId,
      ts: formatTimestamp(null, message.receivedAt),
      text: noticeText(message),
      kind: "notice",
    });
    return;
  }

  if (message.kind !== "data") {
    return;
  }

  const payload = message.payload;
  const text =
    typeof payload?.data === "string"
      ? payload.data
      : typeof payload?.chunk === "string"
        ? payload.chunk
        : "";
  if (!text) {
    return;
  }

  const ts = formatTimestamp(payload?.ts, message.receivedAt);
  if (payload?.replay === true) {
    // Accept tagged history only for the batch this viewer's own start
    // requested, sized by the server-reported count. Live (untagged) frames do
    // not close the window, so an interleaved live frame cannot truncate the
    // history (round-5 U1).
    if (!replayWindow.acceptReplay) {
      return;
    }
    replayWindow.replayAccepted += 1;
    if (
      replayWindow.replayRemaining !== null &&
      replayWindow.replayAccepted >= replayWindow.replayRemaining
    ) {
      replayWindow.acceptReplay = false;
      clearReplayTimer();
    }
  }
  for (const piece of splitLines(text)) {
    appendLine({ id: ++lineId, ts, text: piece, kind: "line" });
  }
}

/** appendLine adds a line to the view, or queues it while paused. */
function appendLine(line: LogLine): void {
  const target = isPaused.value ? pending : lines;
  target.value.push(line);
  if (target.value.length > props.maxLines) {
    target.value.splice(0, target.value.length - props.maxLines);
  }
  scheduleScroll();
}

/** splitLines breaks a chunk into lines, dropping a single trailing newline. */
function splitLines(text: string): string[] {
  const parts = text.split(/\r?\n/);
  if (parts.length > 1 && parts[parts.length - 1] === "") {
    parts.pop();
  }
  return parts;
}

/** formatTimestamp renders a hub timestamp as HH:MM:SS. */
function formatTimestamp(ts: unknown, receivedAt: number): string {
  if (typeof ts === "string" && /^\d{2}:\d{2}:\d{2}$/.test(ts)) {
    return ts;
  }

  let value: Date;
  if (typeof ts === "number") {
    value = new Date(ts < 1e12 ? ts * 1000 : ts);
  } else if (typeof ts === "string" && ts !== "") {
    const parsed = new Date(ts);
    value = Number.isNaN(parsed.getTime()) ? new Date(receivedAt) : parsed;
  } else {
    value = new Date(receivedAt);
  }
  return value.toTimeString().slice(0, 8);
}

/** noticeText extracts the server's own notice copy when provided. */
function noticeText(message: WebSocketMessage): string {
  const payload = message.payload;
  const candidate =
    payload?.message ?? payload?.notice ?? payload?.reason ?? payload?.data;
  if (typeof candidate === "string" && candidate.trim() !== "") {
    return candidate;
  }
  return "Log stream interrupted; reconnecting…";
}

/** scheduleScroll coalesces tail-scroll work across a burst of frames. */
function scheduleScroll(): void {
  if (!isFollowing.value || scrollQueued) {
    return;
  }
  scrollQueued = true;
  void nextTick(() => {
    scrollQueued = false;
    const element = logBody.value;
    if (element && isFollowing.value) {
      element.scrollTop = element.scrollHeight;
    }
  });
}

/** handleScroll disables follow when the reader scrolls away from the tail. */
function handleScroll(): void {
  const element = logBody.value;
  if (!element) {
    return;
  }
  const distanceToBottom =
    element.scrollHeight - element.scrollTop - element.clientHeight;
  if (distanceToBottom <= 24) {
    isFollowing.value = true;
  } else if (isFollowing.value) {
    isFollowing.value = false;
  }
}

/** flushPending appends queued lines to the view in order, respecting maxLines. */
function flushPending(): void {
  flushPendingLines(
    { lines: lines.value, pending: pending.value },
    props.maxLines,
  );
}

/** togglePause freezes the view; resuming flushes queued lines. */
function togglePause(): void {
  isPaused.value = !isPaused.value;
  if (!isPaused.value) {
    flushPending();
  }
  scheduleScroll();
}

/** toggleFollow flips tail-following and snaps to the tail when enabled. */
function toggleFollow(): void {
  isFollowing.value = !isFollowing.value;
  if (isFollowing.value) {
    scheduleScroll();
  }
}

/** clearLines drops the rendered buffer and the shared frame buffer. */
function clearLines(): void {
  // Truncate in place so the per-channel store keeps the same array identity.
  lines.value.length = 0;
  pending.value.length = 0;
  clearStreamBuffer();
}

/** downloadLog exports the visible lines as a plain-text file. */
function downloadLog(): void {
  if (typeof document === "undefined" || lines.value.length === 0) {
    return;
  }
  const content = lines.value
    .map((line) => `${line.ts} ${line.text}`)
    .join("\n");
  const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `${props.containerId || "log"}.log`;
  anchor.click();
  URL.revokeObjectURL(url);
}

/**
 * requestStreamStart asks the control plane to bridge this container's agent
 * log stream into the realtime channel. The viewer only subscribes otherwise,
 * so without this call a raw container produces no frames (B2-1). A failure is
 * surfaced as a notice rather than leaving the drawer silently empty.
 */
function requestStreamStart(): void {
  if (!props.autoStartStream || !props.serverId || !props.containerId) {
    return;
  }
  // Capture the channel generation so a response for a channel the viewer has
  // since left cannot mutate the new channel's replay window or buffer (U1).
  const requestedGeneration = channelGeneration;
  // Decide once per start whether replayed history should render: a late,
  // empty viewer accepts it; a viewer that already has lines (reconnect) does
  // not, so no duplicate lines. The server reports how many tagged frames it
  // published for this start; until the response arrives, tagged frames are
  // accepted optimistically (they can beat the HTTP response).
  clearReplayTimer();
  replayWindow.acceptReplay =
    lines.value.length === 0 && pending.value.length === 0;
  replayWindow.replayRemaining = null;
  replayWindow.replayAccepted = 0;

  if (replayWindow.acceptReplay) {
    replayTimer = setTimeout(() => {
      replayWindow.acceptReplay = false;
      replayTimer = null;
    }, replayWindowMs);
  }

  void startContainerLogStream(props.serverId, props.containerId)
    .then((replay) => {
      if (
        !applyStartSuccess(
          replayWindow,
          requestedGeneration,
          channelGeneration,
          replay,
        )
      ) {
        return;
      }
      if (!replayWindow.acceptReplay) {
        clearReplayTimer();
      }
    })
    .catch((error: unknown) => {
      const notice: LogLine = {
        id: ++lineId,
        ts: formatTimestamp(null, Date.now()),
        text: `Could not start log stream: ${describeContainerError(error)}`,
        kind: "notice",
      };
      if (
        !applyStartFailure(
          replayWindow,
          requestedGeneration,
          channelGeneration,
          { lines: lines.value, pending: pending.value },
          isPaused.value,
          notice,
          props.maxLines,
        )
      ) {
        return;
      }
      clearReplayTimer();
      scheduleScroll();
    });
}

watch(channelName, (next, previous) => {
  // Invalidate any in-flight start/response for the channel being left (U1).
  channelGeneration += 1;
  if (previous) {
    unsubscribeChannel(previous);
  }
  clearStreamBuffer();
  resetReplayState();
  // Re-scope the rendered buffer to the new channel: a switch must not
  // concatenate the two streams, and switching back restores the old one
  // (B2-2 / C4-4).
  const buffer: ChannelLogBuffer<LogLine> = activateChannel(
    channelBuffers,
    previous ?? "",
    next,
    { lines: lines.value, pending: pending.value },
  );
  lines.value = buffer.lines;
  pending.value = buffer.pending;
  isPaused.value = false;
  isFollowing.value = true;
  // A restored buffer may carry lines queued while it was paused; flush them
  // now so they stay in order instead of resurfacing behind later live lines
  // (U2).
  flushPending();
  // The subscribed ack (handleMessage) starts the stream for the new channel.
  subscribeChannel(next);
});

onMounted(() => {
  connectStream();
  // The start call fires on the subscribed ack, not here, so the hub room is
  // populated before the agent tail is published.
  subscribeChannel(channelName.value);
});

onBeforeUnmount(() => {
  clearReplayTimer();
  closeStream(1000, "viewer unmounted");
});
</script>

<template>
  <section class="log-viewer">
    <header v-if="title || subtitle" class="log-viewer__head">
      <div class="log-viewer__heading">
        <h3 v-if="title">{{ title }}</h3>
        <p v-if="subtitle">{{ subtitle }}</p>
      </div>
      <span class="realtime" :class="statusClasses">{{ statusLabel }}</span>
    </header>

    <div class="log-viewer__toolbar">
      <NButton
        size="small"
        secondary
        :aria-pressed="isPaused"
        @click="togglePause"
      >
        {{ isPaused ? "Resume" : "Pause" }}
      </NButton>
      <NButton
        size="small"
        secondary
        :type="isFollowing ? 'primary' : 'default'"
        :aria-pressed="isFollowing"
        @click="toggleFollow"
      >
        {{ isFollowing ? "Following" : "Follow" }}
      </NButton>
      <NButton size="small" secondary @click="clearLines">Clear</NButton>
      <NButton size="small" secondary @click="downloadLog">Download</NButton>
      <span class="log-viewer__count">{{ lines.length }} lines</span>
    </div>

    <!-- role=log + aria-live announce appended lines; tabindex makes the
         scroll region reachable so it can be scrolled with the keyboard
         (B2-7). -->
    <div
      ref="logBody"
      class="log"
      role="log"
      aria-live="polite"
      aria-relevant="additions"
      aria-label="Log output"
      tabindex="0"
      @scroll="handleScroll"
    >
      <p v-if="lines.length === 0" class="log-viewer__empty">
        Waiting for log output…
      </p>
      <div
        v-for="line in lines"
        :key="line.id"
        class="log-line"
        :data-kind="line.kind"
      >
        <span class="t">{{ line.ts }}</span>
        <span class="m">{{ line.text }}</span>
      </div>
    </div>

    <p class="log-viewer__channel">
      Channel: <span class="mono">{{ channelName }}</span>
    </p>
  </section>
</template>

<style scoped>
.log-viewer {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.log-viewer__head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.log-viewer__heading {
  min-width: 0;
  flex: 1 1 auto;
}

.log-viewer__heading h3 {
  margin: 0;
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--fg-2);
}

.log-viewer__heading p {
  margin: 2px 0 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.realtime {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--success-ink);
  background: var(--success-soft);
  border-radius: var(--radius-pill);
  padding: 2px 8px;
  white-space: nowrap;
}

.realtime.is-paused {
  color: var(--warn-ink);
  background: var(--warn-soft);
}

.realtime.is-offline {
  color: var(--muted);
  background: var(--surface-warm);
}

.log-viewer__toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.log-viewer__count {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--muted);
}

.log {
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  line-height: 1.55;
  padding: var(--space-3);
  overflow-y: auto;
  overflow-x: auto;
  min-height: 320px;
  max-height: 56vh;
}

.log-line {
  display: grid;
  grid-template-columns: 78px minmax(0, 1fr);
  gap: var(--space-2);
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--fg);
}

.log-line .t {
  color: var(--muted);
}

.log-line[data-kind="notice"] .m {
  color: var(--warn-ink);
  font-style: italic;
}

.log-viewer__empty {
  margin: 0;
  color: var(--muted);
}

.log-viewer__channel {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--meta);
}

.mono {
  font-family: var(--font-mono);
}
</style>
