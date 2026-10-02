<script setup lang="ts">
import { NButton, NSelect, NTooltip } from "naive-ui";
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";

import { expireSession, isStaleRefreshError, refreshSession } from "../api/http";
import { describeServiceError, serviceLogsPath } from "../api/services";
import { getAccessToken, getRefreshToken } from "../api/token";

/**
 * Live log terminal for one compose service.
 *
 * The control plane streams the node agent's `docker compose logs` as a
 * plain-text chunked response (GET /services/{id}/logs), not over the realtime
 * hub, so this reader consumes the response body directly. Nothing is
 * invented: only bytes the API sends are rendered, and the stream starts when
 * the operator asks for it.
 *
 * Because the reader bypasses the axios instance, it honours the same
 * refresh-once contract itself: a 401 triggers the shared session refresh and
 * one retry before the error is surfaced as "session expired".
 *
 * Known gap, rendered as an explicit stub below: the API has no stored log
 * artifact, so "Download" cannot export anything that is not already on
 * screen.
 */

interface Props {
  serviceId: string;
  /** Compose service names the project declares; empty means all services. */
  services?: string[];
  title?: string;
  maxLines?: number;
}

const props = withDefaults(defineProps<Props>(), {
  services: () => [],
  title: "Service logs",
  maxLines: 2000,
});

type StreamStatus =
  | "idle"
  | "connecting"
  | "streaming"
  | "closed"
  | "error";

/** One rendered log line with a stable identity for the list key. */
interface LogLine {
  id: number;
  text: string;
}

const sessionExpiredMessage = "Your session expired. Please sign in again.";

const status = ref<StreamStatus>("idle");
const error = ref<string | null>(null);
const lines = ref<LogLine[]>([]);
const pending = ref<LogLine[]>([]);
const isPaused = ref(false);
const isFollowing = ref(true);
const selectedService = ref<string>("");
const logBody = ref<HTMLElement | null>(null);

/** The in-flight request; aborted by Stop and on unmount. */
let controller: AbortController | null = null;

/** Monotonic line id so appended lines never reuse a list key. */
let nextLineId = 0;

/** Coalesces tail-scroll work across a burst of chunks. */
let scrollQueued = false;

const serviceOptions = computed<Array<{ label: string; value: string }>>(() => [
  { label: "All services", value: "" },
  ...props.services.map((name) => ({ label: name, value: name })),
]);

const statusLabel = computed<string>(() => {
  if (isPaused.value) {
    return "Paused";
  }
  switch (status.value) {
    case "connecting":
      return "Connecting";
    case "streaming":
      return "Streaming";
    case "closed":
      return "Closed";
    case "error":
      return "Failed";
    default:
      return "Idle";
  }
});

const statusClasses = computed<Record<string, boolean>>(() => ({
  "is-paused": isPaused.value,
  "is-offline": status.value === "idle" || status.value === "closed",
  "is-error": status.value === "error",
}));

/** start opens a followed stream for the selected compose service. */
async function start(): Promise<void> {
  stop();
  lines.value = [];
  pending.value = [];
  error.value = null;
  status.value = "connecting";

  const abort = new AbortController();
  controller = abort;
  try {
    const response = await openStream(abort.signal);
    if (!response.ok || response.body === null) {
      error.value = describeServiceError(
        await toApiError(response),
      );
      status.value = "error";
      return;
    }
    status.value = "streaming";
    await readBody(response.body, abort);
    if (!abort.signal.aborted) {
      status.value = "closed";
    }
  } catch (err) {
    if (abort.signal.aborted) {
      return;
    }
    status.value = "error";
    error.value = describeServiceError(err);
  } finally {
    if (controller === abort) {
      controller = null;
    }
  }
}

/**
 * openStream issues the log request, refreshing the session once on 401. The
 * reader cannot use the axios instance (it consumes a chunked body), so it
 * wires the shared refresh flow manually. A failed refresh clears the stored
 * session and surfaces the session-expired message; a second 401 is mapped by
 * describeServiceError to the same message.
 */
async function openStream(signal: AbortSignal): Promise<Response> {
  const request = (): Promise<Response> =>
    fetch(
      serviceLogsPath(props.serviceId, {
        service: selectedService.value || undefined,
        tail: 200,
        follow: true,
      }),
      {
        headers: { Authorization: `Bearer ${getAccessToken() ?? ""}` },
        signal,
      },
    );

  let response = await request();
  if (response.status === 401) {
    // The session that starts the refresh; a replacement installed before this
    // handler decides to expire must survive.
    const tokenBeforeRefresh = getRefreshToken();
    try {
      await refreshSession();
    } catch (error) {
      const stillCurrent = getRefreshToken() === tokenBeforeRefresh;
      if (!isStaleRefreshError(error) && stillCurrent) {
        // Genuine auth failure for the current session: drop it and redirect to
        // the login page instead of leaving the reader on a dead session.
        expireSession();
        throw new Error(sessionExpiredMessage);
      }
      // A newer session replaced this one mid-refresh: keep it and retry the
      // request with its token below.
    }
    response = await request();
  }
  return response;
}

/** stop aborts the stream; safe to call when idle. */
function stop(): void {
  if (controller !== null) {
    controller.abort();
    controller = null;
  }
  if (status.value === "streaming" || status.value === "connecting") {
    status.value = "closed";
  }
}

/** readBody consumes the chunked body into rendered lines. */
async function readBody(
  body: ReadableStream<Uint8Array>,
  abort: AbortController,
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) {
        break;
      }
      buffer += decoder.decode(value, { stream: true });
      const parts = buffer.split("\n");
      buffer = parts.pop() ?? "";
      for (const line of parts) {
        appendLine(line);
      }
    }
    // Flush the decoder: a trailing multi-byte character may still be held.
    buffer += decoder.decode();
    if (buffer !== "") {
      appendLine(buffer);
    }
  } finally {
    if (abort.signal.aborted) {
      // Cancel the body before releasing the lock: a released reader can no
      // longer cancel the stream, so the abort would leak the connection.
      await reader.cancel().catch(() => undefined);
    }
    reader.releaseLock();
  }
}

/** appendLine adds a line to the view, or queues it while paused. */
function appendLine(text: string): void {
  const line: LogLine = { id: ++nextLineId, text };
  const target = isPaused.value ? pending : lines;
  target.value.push(line);
  if (target.value.length > props.maxLines) {
    target.value.splice(0, target.value.length - props.maxLines);
  }
  scheduleScroll();
}

/** scheduleScroll coalesces tail-scroll work across a burst of chunks. */
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
  isFollowing.value = distanceToBottom <= 24;
}

/** togglePause freezes the view; resuming flushes the queued lines. */
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

/** clearLines drops the rendered buffer. */
function clearLines(): void {
  lines.value = [];
  pending.value = [];
}

/** toApiError maps a failed response to the shared ApiError shape. */
async function toApiError(response: Response): Promise<unknown> {
  const text = await response.text().catch(() => "");
  let message = "";
  try {
    const body = JSON.parse(text) as { message?: string };
    message = body.message ?? "";
  } catch {
    message = text.trim();
  }
  return { status: response.status, message, cause: null };
}

watch(selectedService, () => {
  if (status.value === "streaming" || status.value === "connecting") {
    void start();
  }
});

onBeforeUnmount(() => {
  stop();
});
</script>

<template>
  <section class="service-logs">
    <div class="service-logs__toolbar">
      <NSelect
        v-model:value="selectedService"
        :options="serviceOptions"
        size="small"
        style="width: 200px"
        aria-label="Compose service to stream"
      />
      <NButton
        v-if="status === 'streaming' || status === 'connecting'"
        size="small"
        type="error"
        secondary
        @click="stop"
      >
        Stop
      </NButton>
      <NButton v-else size="small" type="primary" secondary @click="start">
        Stream logs
      </NButton>
      <NButton
        size="small"
        secondary
        :disabled="status !== 'streaming'"
        @click="togglePause"
      >
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
      <NTooltip trigger="hover">
        <template #trigger>
          <NButton size="small" secondary disabled>Download</NButton>
        </template>
        Backend pending: the API serves a live stream only, so there is no
        stored log file to download.
      </NTooltip>
      <span class="realtime" :class="statusClasses">{{ statusLabel }}</span>
      <span class="service-logs__count">{{ lines.length }} lines</span>
    </div>

    <p v-if="error" class="service-logs__error">{{ error }}</p>

    <div ref="logBody" class="log mono" @scroll="handleScroll">
      <p v-if="lines.length === 0" class="service-logs__empty">
        {{
          status === "idle"
            ? "No stream open. Select a compose service and start streaming."
            : "Waiting for log output…"
        }}
      </p>
      <div v-for="line in lines" :key="line.id" class="log-line">
        {{ line.text }}
      </div>
    </div>

    <p class="service-logs__channel">
      Channel: <span class="mono">GET /api/v1/services/{{
        serviceId.slice(0, 8)
      }}…/logs?follow=true</span> · agent runs
      <span class="mono">docker compose logs -f</span>.
    </p>
  </section>
</template>

<style scoped>
.service-logs {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.service-logs__toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.service-logs__count {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--muted);
}

.service-logs__error {
  margin: 0;
  font-size: var(--text-sm);
  color: var(--danger-ink);
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

.realtime.is-error {
  color: var(--danger-ink);
  background: var(--danger-soft);
}

.log {
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  font-size: var(--text-xs);
  line-height: 1.55;
  padding: var(--space-3);
  overflow: auto;
  min-height: 240px;
  max-height: 52vh;
}

.log-line {
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--fg);
}

.service-logs__empty {
  margin: 0;
  color: var(--muted);
}

.service-logs__channel {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--meta);
}

.mono {
  font-family: var(--font-mono);
}
</style>
