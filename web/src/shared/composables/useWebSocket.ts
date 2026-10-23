import { computed, getCurrentScope, onScopeDispose, ref } from "vue";
import type { ComputedRef, Ref } from "vue";

export type WebSocketStatus =
  | "idle"
  | "connecting"
  | "open"
  | "reconnecting"
  | "closed"
  | "error";

export type WebSocketMessageKind = "data" | "notice" | "denied" | "unknown";

export interface WebSocketMessage {
  raw: string;
  payload: Record<string, unknown> | null;
  channel: string | null;
  kind: WebSocketMessageKind;
  receivedAt: number;
}

export interface UseWebSocketOptions {
  url: string;
  token?: string | null | (() => string | null);
  maxRetries?: number;
  baseDelayMs?: number;
  maxDelayMs?: number;
  jitter?: boolean;
  bufferLimit?: number;
  bufferByteLimit?: number;
  autoConnect?: boolean;
  onMessage?: (_message: WebSocketMessage) => void;
  onStatusChange?: (_status: WebSocketStatus) => void;
  onReconnectFailed?: () => void | Promise<unknown>;
}

export interface UseWebSocketReturn {
  status: Ref<WebSocketStatus>;
  isOpen: ComputedRef<boolean>;
  retryCount: Ref<number>;
  lastError: Ref<string | null>;
  messages: Ref<WebSocketMessage[]>;
  connect: () => void;
  close: (_code?: number, _reason?: string) => void;
  send: (_data: unknown) => boolean;
  subscribe: (_channel: string) => void;
  unsubscribe: (_channel: string) => void;
  clearBuffer: () => void;
}

const defaultMaxRetries = 5;
const defaultBaseDelayMs = 500;
const defaultMaxDelayMs = 15_000;
const defaultBufferLimit = 2000;
const defaultBufferByteLimit = 2 * 1024 * 1024;

const socketOpening = 0;
const socketOpen = 1;

export function computeBackoffDelay(
  attempt: number,
  baseDelayMs: number = defaultBaseDelayMs,
  maxDelayMs: number = defaultMaxDelayMs,
  jitter = false,
  random: () => number = Math.random,
): number {
  const exponent = Math.max(1, Math.floor(attempt));
  const capped = Math.min(baseDelayMs * 2 ** (exponent - 1), maxDelayMs);
  if (!jitter) {
    return capped;
  }
  return Math.round(capped / 2 + random() * (capped / 2));
}

export function buildWebSocketUrl(url: string, token?: string | null): string {
  const location = typeof window === "undefined" ? null : window.location;

  let resolved: URL;
  if (/^wss?:\/\//i.test(url)) {
    resolved = new URL(url);
  } else if (/^https?:\/\//i.test(url)) {
    resolved = new URL(url.replace(/^http/i, "ws"));
  } else if (location) {
    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    resolved = new URL(url, `${scheme}//${location.host}`);
  } else {
    resolved = new URL(url, "ws://localhost");
  }

  if (token) {
    resolved.searchParams.set("token", token);
  }
  return resolved.toString();
}

function describeError(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return typeof error === "string" ? error : "Realtime connection failed.";
}

export function useWebSocket(options: UseWebSocketOptions): UseWebSocketReturn {
  const maxRetries = options.maxRetries ?? defaultMaxRetries;
  const baseDelayMs = options.baseDelayMs ?? defaultBaseDelayMs;
  const maxDelayMs = options.maxDelayMs ?? defaultMaxDelayMs;
  const jitter = options.jitter ?? false;
  const bufferLimit = options.bufferLimit ?? defaultBufferLimit;
  const bufferByteLimit = options.bufferByteLimit ?? defaultBufferByteLimit;

  const status = ref<WebSocketStatus>("idle");
  const retryCount = ref(0);
  const lastError = ref<string | null>(null);
  const messages = ref<WebSocketMessage[]>([]);
  const isOpen = computed<boolean>(() => status.value === "open");

  let socket: WebSocket | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let shouldReconnect = false;
  let generation = 0;
  let bufferedBytes = 0;
  let refreshTriggered = false;
  const channels = new Set<string>();

  function resolveToken(): string | null {
    return typeof options.token === "function"
      ? options.token()
      : options.token ?? null;
  }

  function setStatus(next: WebSocketStatus): void {
    if (status.value === next) {
      return;
    }
    status.value = next;
    options.onStatusChange?.(next);
  }

  function pushMessage(message: WebSocketMessage): void {
    const buffer = messages.value;
    buffer.push(message);
    bufferedBytes += message.raw.length;
    while (
      buffer.length > bufferLimit ||
      (buffer.length > 1 && bufferedBytes > bufferByteLimit)
    ) {
      const dropped = buffer.shift();
      if (!dropped) {
        break;
      }
      bufferedBytes -= dropped.raw.length;
    }
    options.onMessage?.(message);
  }

  function handleFrame(raw: string): void {
    let payload: Record<string, unknown> | null = null;
    try {
      const parsed: unknown = JSON.parse(raw);
      if (parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)) {
        payload = parsed as Record<string, unknown>;
      }
    } catch {
      payload = null;
    }

    const type = typeof payload?.type === "string" ? payload.type : "";
    let kind: WebSocketMessageKind = "unknown";
    if (type === "denied") {
      kind = "denied";
    } else if (type === "notice" || type === "disconnect") {
      kind = "notice";
    } else if (
      type === "log" ||
      typeof payload?.data === "string" ||
      typeof payload?.chunk === "string"
    ) {
      kind = "data";
    }

    pushMessage({
      raw,
      payload,
      channel: typeof payload?.channel === "string" ? payload.channel : null,
      kind,
      receivedAt: Date.now(),
    });
  }

  function clearRetryTimer(): void {
    if (retryTimer !== null) {
      clearTimeout(retryTimer);
      retryTimer = null;
    }
  }

  function sendChannelFrame(
    type: "subscribe" | "unsubscribe",
    channel: string,
  ): void {
    if (!socket || socket.readyState !== socketOpen) {
      return;
    }
    socket.send(JSON.stringify({ [type]: channel }));
  }

  function scheduleReconnect(): void {
    if (retryCount.value >= maxRetries) {
      lastError.value ??= "Unable to reach the realtime stream.";
      shouldReconnect = false;
      setStatus("error");
      return;
    }

    retryCount.value += 1;
    if (!refreshTriggered && options.onReconnectFailed) {
      refreshTriggered = true;
      try {
        void Promise.resolve(options.onReconnectFailed()).catch(() => {});
      } catch {
      }
    }
    const delay = computeBackoffDelay(
      retryCount.value,
      baseDelayMs,
      maxDelayMs,
      jitter,
    );
    setStatus("reconnecting");
    clearRetryTimer();
    retryTimer = setTimeout(() => {
      retryTimer = null;
      openSocket();
    }, delay);
  }

  function openSocket(): void {
    if (typeof WebSocket === "undefined") {
      lastError.value = "WebSocket is not supported in this environment.";
      shouldReconnect = false;
      setStatus("error");
      return;
    }

    clearRetryTimer();
    const currentGeneration = ++generation;
    if (socket) {
      const previous = socket;
      socket = null;
      try {
        previous.close(1000, "superseded");
      } catch {
      }
    }
    setStatus(retryCount.value > 0 ? "reconnecting" : "connecting");

    let ws: WebSocket;
    try {
      ws = new WebSocket(buildWebSocketUrl(options.url, resolveToken()));
    } catch (error) {
      lastError.value = describeError(error);
      scheduleReconnect();
      return;
    }
    socket = ws;

    let settled = false;
    const fail = (): void => {
      if (currentGeneration !== generation || settled) {
        return;
      }
      settled = true;
      socket = null;
      try {
        ws.close();
      } catch {
      }
      if (!shouldReconnect) {
        setStatus("closed");
        return;
      }
      scheduleReconnect();
    };

    ws.onopen = () => {
      if (currentGeneration !== generation) {
        return;
      }
      settled = false;
      retryCount.value = 0;
      refreshTriggered = false;
      lastError.value = null;
      setStatus("open");
      for (const channel of channels) {
        sendChannelFrame("subscribe", channel);
      }
    };

    ws.onmessage = (event: MessageEvent) => {
      if (currentGeneration !== generation) {
        return;
      }
      if (typeof event.data === "string") {
        handleFrame(event.data);
      }
    };

    ws.onerror = () => {
      if (currentGeneration !== generation) {
        return;
      }
      lastError.value ??= "Realtime connection error.";
      fail();
    };

    ws.onclose = (event: CloseEvent) => {
      if (currentGeneration !== generation) {
        return;
      }
      if (!lastError.value && typeof event.reason === "string" && event.reason) {
        lastError.value = event.reason;
      }
      fail();
    };
  }

  function connect(): void {
    if (status.value === "open" || status.value === "connecting") {
      return;
    }
    shouldReconnect = true;
    retryCount.value = 0;
    refreshTriggered = false;
    lastError.value = null;
    openSocket();
  }

  function close(code = 1000, reason = "client closed"): void {
    shouldReconnect = false;
    clearRetryTimer();
    generation += 1;

    const ws = socket;
    socket = null;
    if (ws && (ws.readyState === socketOpen || ws.readyState === socketOpening)) {
      try {
        ws.close(code, reason);
      } catch {
      }
    }
    setStatus("closed");
  }

  function send(data: unknown): boolean {
    if (!socket || socket.readyState !== socketOpen) {
      return false;
    }
    socket.send(typeof data === "string" ? data : JSON.stringify(data));
    return true;
  }

  function subscribe(channel: string): void {
    if (!channel) {
      return;
    }
    channels.add(channel);
    sendChannelFrame("subscribe", channel);
  }

  function unsubscribe(channel: string): void {
    if (!channels.delete(channel)) {
      return;
    }
    sendChannelFrame("unsubscribe", channel);
  }

  function clearBuffer(): void {
    messages.value = [];
    bufferedBytes = 0;
  }

  if (getCurrentScope()) {
    onScopeDispose(() => close(1000, "scope disposed"));
  }

  if (options.autoConnect) {
    connect();
  }

  return {
    status,
    isOpen,
    retryCount,
    lastError,
    messages,
    connect,
    close,
    send,
    subscribe,
    unsubscribe,
    clearBuffer,
  };
}
