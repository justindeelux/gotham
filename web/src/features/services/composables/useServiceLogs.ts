import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import type { ComputedRef, Ref } from "vue";

import { expireSession, isStaleRefreshError, refreshSession } from "@/shared/api/http";
import { describeServiceError, serviceLogsPath } from "@/features/services/api/services";
import { activeLocale, i18n } from "@/shared/i18n";
import { getAccessToken, getRefreshToken } from "@/shared/api/token";

export type StreamStatus =
  | "idle"
  | "connecting"
  | "streaming"
  | "closed"
  | "error";

/** One rendered log line with a stable identity for the list key. */
export interface LogLine {
  id: number;
  text: string;
}

export interface ServiceLogsSource {
  serviceId: Ref<string>;
  services: Ref<string[]>;
  maxLines: Ref<number>;
}

/**
 * Shared state for the live log terminal (see ServiceLogs.vue): the
 * control plane streams the node agent's `docker compose logs` as a
 * plain-text chunked response (GET /services/{id}/logs), not over the realtime
 * hub, so this reader consumes the response body directly.
 */
export interface ServiceLogsState {
  status: Ref<StreamStatus>;
  error: ComputedRef<string | null>;
  lines: Ref<LogLine[]>;
  isPaused: Ref<boolean>;
  isFollowing: Ref<boolean>;
  selectedService: Ref<string>;
  logBody: Ref<HTMLElement | null>;
  serviceOptions: ComputedRef<Array<{ label: string; value: string }>>;
  statusLabel: ComputedRef<string>;
  statusClasses: ComputedRef<Record<string, boolean>>;
  start: () => Promise<void>;
  stop: () => void;
  togglePause: () => void;
  toggleFollow: () => void;
  clearLines: () => void;
  handleScroll: () => void;
}

/**
 * sessionExpiredKey is the curated summary stored for a dead session; the
 * display text resolves in the active locale through streamError below.
 */
const sessionExpiredKey = "services.errors.sessionExpired";

/**
 * useServiceLogs owns the log stream lifecycle: open / consume / pause /
 * follow. Because the reader bypasses the axios instance, it honours the same
 * refresh-once contract itself: a 401 triggers the shared session refresh and
 * one retry before the error is surfaced as "session expired".
 */
export function useServiceLogs(source: ServiceLogsSource): ServiceLogsState {
  const status = ref<StreamStatus>("idle");
  /**
   * streamFailure retains the raw stream refusal; error derives its display
   * text in the active locale so an open failure refreshes on a switch.
   * A dead session stores a marker resolved through the services catalog.
   */
  const streamFailure: Ref<unknown> = ref(null);
  const error = computed<string | null>(() => {
    if (streamFailure.value === null) {
      return null;
    }
    // Tracks the locale when called during render or inside a computed.
    void activeLocale.value;
    const failure = streamFailure.value;
    if (
      failure === sessionExpiredKey ||
      (failure instanceof Error && failure.message === sessionExpiredKey)
    ) {
      return String(i18n.global.t(sessionExpiredKey));
    }
    return describeServiceError(failure);
  });
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

  const serviceOptions = computed<Array<{ label: string; value: string }>>(() => {
    // Tracks the locale when called during render or inside a computed.
    void activeLocale.value;
    return [
      { label: String(i18n.global.t("services.logs.allServices")), value: "" },
      ...source.services.value.map((name) => ({ label: name, value: name })),
    ];
  });

  const statusLabel = computed<string>(() => {
    // Tracks the locale when called during render or inside a computed.
    void activeLocale.value;
    const text = (key: string): string => String(i18n.global.t(key));
    if (isPaused.value) {
      return text("services.logs.status.paused");
    }
    switch (status.value) {
      case "connecting":
        return text("services.logs.status.connecting");
      case "streaming":
        return text("services.logs.status.streaming");
      case "closed":
        return text("services.logs.status.closed");
      case "error":
        return text("services.logs.status.failed");
      default:
        return text("services.logs.status.idle");
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
    streamFailure.value = null;
    status.value = "connecting";

    const abort = new AbortController();
    controller = abort;
    try {
      const response = await openStream(abort.signal);
      if (!response.ok || response.body === null) {
        streamFailure.value = await toApiError(response);
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
      streamFailure.value = err;
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
        serviceLogsPath(source.serviceId.value, {
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
          throw new Error(sessionExpiredKey, { cause: error });
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
    if (target.value.length > source.maxLines.value) {
      target.value.splice(0, target.value.length - source.maxLines.value);
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
      if (lines.value.length > source.maxLines.value) {
        lines.value.splice(0, lines.value.length - source.maxLines.value);
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
    let message: string;
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

  return {
    status,
    error,
    lines,
    isPaused,
    isFollowing,
    selectedService,
    logBody,
    serviceOptions,
    statusLabel,
    statusClasses,
    start,
    stop,
    togglePause,
    toggleFollow,
    clearLines,
    handleScroll,
  };
}
