
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { Ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  describeContainerError,
  startContainerLogStream,
} from "@/features/servers/api/containers";
import { refreshSession } from "@/shared/api/http";
import { getAccessToken } from "@/shared/api/token";
import {
  activateChannel,
  applyStartFailure,
  applyStartSuccess,
  createChannelLogBufferStore,
  flushPendingLines,
  isFrameForChannel,
} from "@/shared/composables/logChannelBuffers";
import type {
  ChannelLogBuffer,
  ReplayWindow,
} from "@/shared/composables/logChannelBuffers";
import type { WebSocketMessage } from "@/shared/composables/useWebSocket";
import { useWebSocket } from "@/shared/composables/useWebSocket";
import {
  formatTimestamp,
  noticeText,
  splitLines,
} from "@/features/servers/utils/logFormat";
import type { LogLine } from "@/features/servers/utils/logFormat";

export interface LogStreamProps {
  serverId: string;
  containerId: string;
  channel: string;
  autoStartStream: boolean;
  wsPath: string;
  maxLines: number;
}

export function useLogStream(props: LogStreamProps, logBody: Ref<HTMLElement | null>) {
  const { locale, t } = useI18n();
  const lines = ref<LogLine[]>([]);
  const pending = ref<LogLine[]>([]);
  const isPaused = ref(false);
  const isFollowing = ref(true);
  const channelBuffers = createChannelLogBufferStore<LogLine>();

  let lineId = 0;
  let scrollQueued = false;
  const replayWindow: ReplayWindow = {
    acceptReplay: false,
    replayRemaining: null,
    replayAccepted: 0,
  };
  let replayTimer: ReturnType<typeof setTimeout> | null = null;
  let channelGeneration = 0;

  const replayWindowMs = 5_000;

  function clearReplayTimer(): void {
    if (replayTimer !== null) {
      clearTimeout(replayTimer);
      replayTimer = null;
    }
  }

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
    onReconnectFailed: refreshSession,
  });

  const statusLabel = computed<string>(() => {
    if (isPaused.value) {
      return t("servers.logs.statusPaused");
    }
    switch (streamStatus.value) {
      case "open":
        return t("servers.logs.statusStreaming");
      case "connecting":
        return t("servers.logs.statusConnecting");
      case "reconnecting":
        return t("servers.logs.statusReconnecting");
      case "error":
        return t("servers.logs.statusOffline");
      case "closed":
        return t("servers.logs.statusClosed");
      default:
        return t("servers.logs.statusIdle");
    }
  });

  const statusClasses = computed<Record<string, boolean>>(() => ({
    "is-paused": isPaused.value || streamStatus.value === "reconnecting",
    "is-offline":
      streamStatus.value === "error" ||
      streamStatus.value === "closed" ||
      streamStatus.value === "idle",
  }));

  function handleMessage(message: WebSocketMessage): void {
    if (!isFrameForChannel(message.channel, channelName.value)) {
      return;
    }

    if (
      message.payload?.type === "subscribed" &&
      message.channel === channelName.value
    ) {
      requestStreamStart();
      return;
    }

    if (message.payload?.type === "denied") {
      const reason =
        typeof message.payload.data === "string" ? message.payload.data : "";
      appendLine({
        id: ++lineId,
        ts: formatTimestamp(null, message.receivedAt),
        text: reason
          ? t("servers.logs.subscriptionDeniedReason", { reason })
          : t("servers.logs.subscriptionDenied"),
        kind: "notice",
      });
      return;
    }

    if (message.payload?.type === "resumed") {
      appendLine({
        id: ++lineId,
        ts: formatTimestamp(null, message.receivedAt),
        text: t("servers.logs.streamResumed"),
        kind: "notice",
      });
      return;
    }

    if (message.payload?.type === "replay_end") {
      replayWindow.acceptReplay = false;
      clearReplayTimer();
      return;
    }

    if (message.kind === "notice") {
      appendLine({
        id: ++lineId,
        ts: formatTimestamp(null, message.receivedAt),
        text: noticeText(message, locale.value),
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

  function appendLine(line: LogLine): void {
    const target = isPaused.value ? pending : lines;
    target.value.push(line);
    if (target.value.length > props.maxLines) {
      target.value.splice(0, target.value.length - props.maxLines);
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
    if (distanceToBottom <= 24) {
      isFollowing.value = true;
    } else if (isFollowing.value) {
      isFollowing.value = false;
    }
  }

  function flushPending(): void {
    flushPendingLines(
      { lines: lines.value, pending: pending.value },
      props.maxLines,
    );
  }

  function togglePause(): void {
    isPaused.value = !isPaused.value;
    if (!isPaused.value) {
      flushPending();
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
    lines.value.length = 0;
    pending.value.length = 0;
    clearStreamBuffer();
  }

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

  function requestStreamStart(): void {
    if (!props.autoStartStream || !props.serverId || !props.containerId) {
      return;
    }
    const requestedGeneration = channelGeneration;
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
          text: t("servers.logs.streamStartFailed", { detail: describeContainerError(error) }),
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
    channelGeneration += 1;
    if (previous) {
      unsubscribeChannel(previous);
    }
    clearStreamBuffer();
    resetReplayState();
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
    flushPending();
    subscribeChannel(next);
  });

  onMounted(() => {
    connectStream();
    subscribeChannel(channelName.value);
  });

  onBeforeUnmount(() => {
    clearReplayTimer();
    closeStream(1000, "viewer unmounted");
  });

  return {
    lines,
    isPaused,
    isFollowing,
    channelName,
    statusLabel,
    statusClasses,
    handleScroll,
    togglePause,
    toggleFollow,
    clearLines,
    downloadLog,
  };
}
