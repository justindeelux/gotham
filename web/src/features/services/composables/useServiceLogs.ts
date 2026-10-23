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

export interface LogLine {
  id: number;
  text: string;
}

export interface ServiceLogsSource {
  serviceId: Ref<string>;
  services: Ref<string[]>;
  maxLines: Ref<number>;
}

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

const sessionExpiredKey = "services.errors.sessionExpired";

export function useServiceLogs(source: ServiceLogsSource): ServiceLogsState {
  const status = ref<StreamStatus>("idle");
  const streamFailure: Ref<unknown> = ref(null);
  const error = computed<string | null>(() => {
    if (streamFailure.value === null) {
      return null;
    }
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

  let controller: AbortController | null = null;

  let nextLineId = 0;

  let scrollQueued = false;

  const serviceOptions = computed<Array<{ label: string; value: string }>>(() => {
    void activeLocale.value;
    return [
      { label: String(i18n.global.t("services.logs.allServices")), value: "" },
      ...source.services.value.map((name) => ({ label: name, value: name })),
    ];
  });

  const statusLabel = computed<string>(() => {
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
      const tokenBeforeRefresh = getRefreshToken();
      try {
        await refreshSession();
      } catch (error) {
        const stillCurrent = getRefreshToken() === tokenBeforeRefresh;
        if (!isStaleRefreshError(error) && stillCurrent) {
          expireSession();
          throw new Error(sessionExpiredKey, { cause: error });
        }
      }
      response = await request();
    }
    return response;
  }

  function stop(): void {
    if (controller !== null) {
      controller.abort();
      controller = null;
    }
    if (status.value === "streaming" || status.value === "connecting") {
      status.value = "closed";
    }
  }

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
      buffer += decoder.decode();
      if (buffer !== "") {
        appendLine(buffer);
      }
    } finally {
      if (abort.signal.aborted) {
        await reader.cancel().catch(() => undefined);
      }
      reader.releaseLock();
    }
  }

  function appendLine(text: string): void {
    const line: LogLine = { id: ++nextLineId, text };
    const target = isPaused.value ? pending : lines;
    target.value.push(line);
    if (target.value.length > source.maxLines.value) {
      target.value.splice(0, target.value.length - source.maxLines.value);
    }
    scheduleScroll();
  }

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

  function handleScroll(): void {
    const element = logBody.value;
    if (!element) {
      return;
    }
    const distanceToBottom =
      element.scrollHeight - element.scrollTop - element.clientHeight;
    isFollowing.value = distanceToBottom <= 24;
  }

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

  function toggleFollow(): void {
    isFollowing.value = !isFollowing.value;
    if (isFollowing.value) {
      scheduleScroll();
    }
  }

  function clearLines(): void {
    lines.value = [];
    pending.value = [];
  }

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
