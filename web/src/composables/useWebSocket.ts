import { computed, getCurrentScope, onScopeDispose, ref } from "vue";
import type { ComputedRef, Ref } from "vue";

/**
 * Reconnecting WebSocket client for the Gotham realtime hub.
 *
 * Wire contract (as merged on main in BE-3.2, `internal/server/ws`):
 *   · endpoint: `WS /api/v1/ws?token=<jwt>`
 *   · subscribe: `{ "subscribe": "logs:{serverID}:{containerID}" }`
 *   · unsubscribe: `{ "unsubscribe": "logs:{serverID}:{containerID}" }`
 *   · frames: `{ channel, type: "log" | "disconnect" | "subscribed", data }`
 *     — `log` carries one log chunk in `data`, `disconnect` is a stream-loss
 *     notice (reason in `data`), `subscribed` acknowledges a subscription.
 *   · preselect alternative: `?channel=…` or `?server=…&container=…`
 *
 * The parser also tolerates the pre-merge draft shape (`type: "notice"`,
 * `data | chunk`, `{ type, channel }` control frames) so it keeps working
 * across the BE-3.2 rollout boundary.
 *
 * The composable owns the socket lifecycle: exponential-backoff reconnects,
 * automatic re-subscription after a reconnect, a capped message buffer, and an
 * explicit {@link UseWebSocketReturn.close} that suppresses any further retry.
 */

/** Connection lifecycle exposed by {@link useWebSocket}. */
export type WebSocketStatus =
  | "idle"
  | "connecting"
  | "open"
  | "reconnecting"
  | "closed"
  | "error";

/** Classification derived from the frame payload shape. */
export type WebSocketMessageKind = "data" | "notice" | "denied" | "unknown";

/** One buffered frame from the realtime hub. */
export interface WebSocketMessage {
  /** Frame text exactly as received, before parsing. */
  raw: string;
  /** Parsed JSON object, or null when the frame was not valid JSON. */
  payload: Record<string, unknown> | null;
  /** Channel the payload was published on, when present. */
  channel: string | null;
  /** Frame classification: log data, a stream notice, or unrecognised. */
  kind: WebSocketMessageKind;
  /** Receipt time in milliseconds since the epoch. */
  receivedAt: number;
}

/** Options accepted by {@link useWebSocket}. */
export interface UseWebSocketOptions {
  /** Endpoint path (e.g. `/api/v1/ws`) or an absolute `ws(s)://` URL. */
  url: string;
  /**
   * JWT appended as the `token` query parameter. A getter is re-resolved on
   * every (re)connect, so a reconnect after the access-token TTL picks up the
   * token the HTTP layer refreshed (B2-3).
   */
  token?: string | null | (() => string | null);
  /** Maximum consecutive reconnect attempts before the error state. */
  maxRetries?: number;
  /** First backoff delay in milliseconds. */
  baseDelayMs?: number;
  /** Upper bound for the backoff delay in milliseconds. */
  maxDelayMs?: number;
  /** Apply full jitter to the backoff delay to avoid reconnect stampedes. */
  jitter?: boolean;
  /** Maximum number of frames retained in the buffer. */
  bufferLimit?: number;
  /** Approximate byte ceiling for the retained frames (B2-9). */
  bufferByteLimit?: number;
  /** Connect as soon as the composable is created. */
  autoConnect?: boolean;
  /** Called for every frame received, after it enters the buffer. */
  onMessage?: (_message: WebSocketMessage) => void;
  /** Called for every status transition. */
  onStatusChange?: (_status: WebSocketStatus) => void;
  /**
   * Best-effort hook invoked at most once per reconnect streak, before the
   * second attempt is scheduled. It exists so an expired session can be
   * refreshed (the token getter then reads the rotated token); rejections are
   * swallowed so a failed refresh never blocks reconnection (U4).
   */
  onReconnectFailed?: () => void | Promise<unknown>;
}

/** Reactive handles returned by {@link useWebSocket}. */
export interface UseWebSocketReturn {
  /** Current connection lifecycle state. */
  status: Ref<WebSocketStatus>;
  /** True while the socket is open. */
  isOpen: ComputedRef<boolean>;
  /** Consecutive failed attempts since the last successful connection. */
  retryCount: Ref<number>;
  /** Last transport error message, cleared on a successful connection. */
  lastError: Ref<string | null>;
  /** Capped buffer of received frames, oldest first. */
  messages: Ref<WebSocketMessage[]>;
  /** Open the socket (resets the retry budget). No-op when already open. */
  connect: () => void;
  /** Close explicitly and stop reconnecting. */
  close: (_code?: number, _reason?: string) => void;
  /** Send a frame; returns false when the socket is not open. */
  send: (_data: unknown) => boolean;
  /** Subscribe to a channel and remember it for automatic re-subscription. */
  subscribe: (_channel: string) => void;
  /** Unsubscribe from a channel. */
  unsubscribe: (_channel: string) => void;
  /** Drop every buffered frame. */
  clearBuffer: () => void;
}

/** Default retry budget before the client settles into the error state. */
const defaultMaxRetries = 5;
/** Default first backoff delay in milliseconds. */
const defaultBaseDelayMs = 500;
/** Default backoff ceiling in milliseconds. */
const defaultMaxDelayMs = 15_000;
/**
 * Default capped-buffer size, matching the log viewer's line cap. The frame
 * cap is intentional; {@link defaultBufferByteLimit} guards it against a burst
 * of oversized chunks, which the frame count alone cannot bound.
 */
const defaultBufferLimit = 2000;
/** Default approximate byte ceiling for the retained frames (2 MiB). */
const defaultBufferByteLimit = 2 * 1024 * 1024;

/** WebSocket readyState constants, kept local so this module never needs the global. */
const socketOpening = 0;
const socketOpen = 1;

/**
 * computeBackoffDelay returns the exponential delay for a reconnect attempt.
 * Attempt 1 yields baseDelayMs; each further attempt doubles until maxDelayMs.
 * When jitter is enabled the delay is randomized into [cap / 2, cap].
 */
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

/**
 * buildWebSocketUrl resolves the endpoint and appends the auth token.
 * Relative paths resolve against the current origin using the ws/wss scheme;
 * absolute `ws(s)://` or `http(s)://` URLs are accepted as-is.
 */
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

/** describeError maps an unknown thrown value to a readable message. */
function describeError(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return typeof error === "string" ? error : "Realtime connection failed.";
}

/** useWebSocket creates a reconnecting realtime client. */
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
  /** Bumped on close so in-flight handlers from a stale socket are ignored. */
  let generation = 0;
  /** Approximate bytes currently retained in {@link messages}. */
  let bufferedBytes = 0;
  /** True once {@link UseWebSocketOptions.onReconnectFailed} ran this streak. */
  let refreshTriggered = false;
  const channels = new Set<string>();

  /** resolveToken reads the configured token, re-running a getter per connect. */
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
    // Drop oldest frames until both the frame cap and the byte guard hold. The
    // newest frame is always kept whole, even when it alone exceeds the byte
    // ceiling, so a burst cannot evict the line just received.
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
    // Merged BE-3.2 control frame: `{ "subscribe": "<channel>" }`.
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
    // On a repeated failure give the session one chance to refresh before the
    // next attempt; the token getter then reads the rotated token (U4). This is
    // fire-and-forget and guarded so it can fire at most once per streak.
    if (!refreshTriggered && options.onReconnectFailed) {
      refreshTriggered = true;
      try {
        void Promise.resolve(options.onReconnectFailed()).catch(() => {});
      } catch {
        // A synchronous throw in the hook must not break reconnection.
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
    // Single-socket invariant (B2-10): if a connect/open supersedes a socket
    // that is still CONNECTING (e.g. connect() called during a retry backoff),
    // tear it down first so its handlers are invalidated and it cannot leak.
    if (socket) {
      const previous = socket;
      socket = null;
      try {
        previous.close(1000, "superseded");
      } catch {
        // Already closing/closed; closing is best-effort cleanup only.
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

    // A failed connection may surface as `error`, as `close`, or (in some
    // runtimes, e.g. a refused Node socket) as `error` alone, so both paths
    // funnel through one idempotent handler to guarantee exactly one retry.
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
        // Already failed; closing is best-effort cleanup only.
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
    // Invalidate handlers from the socket being torn down before closing it.
    generation += 1;

    const ws = socket;
    socket = null;
    if (ws && (ws.readyState === socketOpen || ws.readyState === socketOpening)) {
      try {
        ws.close(code, reason);
      } catch {
        // A socket that is already closing/closed can throw; nothing to do.
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
