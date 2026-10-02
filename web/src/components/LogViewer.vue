<script setup lang="ts">
import { NButton } from "naive-ui";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";

import {
  describeContainerError,
  startContainerLogStream,
} from "../api/containers";
import { getAccessToken } from "../api/token";
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

let lineId = 0;
let scrollQueued = false;

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
  token: getAccessToken(),
  onMessage: handleMessage,
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
  // Replayed history must not duplicate lines on a viewer that already has
  // content (a reconnect re-POSTs the start, and a second viewer's replay is
  // broadcast to the room). Skip it unless this viewer is still empty
  // (round-2 U1).
  if (
    payload?.replay === true &&
    (lines.value.length > 0 || pending.value.length > 0)
  ) {
    return;
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

/** togglePause freezes the view; resuming flushes queued lines. */
function togglePause(): void {
  isPaused.value = !isPaused.value;
  if (!isPaused.value && pending.value.length > 0) {
    lines.value.push(...pending.value);
    pending.value = [];
    if (lines.value.length > props.maxLines) {
      lines.value.splice(0, lines.value.length - props.maxLines);
    }
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
  lines.value = [];
  pending.value = [];
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
  void startContainerLogStream(props.serverId, props.containerId).catch(
    (error: unknown) => {
      appendLine({
        id: ++lineId,
        ts: formatTimestamp(null, Date.now()),
        text: `Could not start log stream: ${describeContainerError(error)}`,
        kind: "notice",
      });
    },
  );
}

watch(channelName, (next, previous) => {
  if (previous) {
    unsubscribeChannel(previous);
  }
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
      <NButton size="small" secondary @click="togglePause">
        {{ isPaused ? "Resume" : "Pause" }}
      </NButton>
      <NButton
        size="small"
        secondary
        :type="isFollowing ? 'primary' : 'default'"
        @click="toggleFollow"
      >
        {{ isFollowing ? "Following" : "Follow" }}
      </NButton>
      <NButton size="small" secondary @click="clearLines">Clear</NButton>
      <NButton size="small" secondary @click="downloadLog">Download</NButton>
      <span class="log-viewer__count">{{ lines.length }} lines</span>
    </div>

    <div ref="logBody" class="log" @scroll="handleScroll">
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
