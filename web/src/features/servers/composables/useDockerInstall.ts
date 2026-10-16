// Docker-install log stream for the Add-server wizard (JUS-187).
//
// The control plane runs the allow-listed package-manager install over the
// wizard's SSH session and streams the combined output as a plain-text chunked
// response (POST /servers/{id}/docker-install), so this reader consumes the
// response body directly with `fetch` (axios cannot stream a browser body).
// The node's agent is not installed at this point, so the App Store path is
// not reachable yet — this pre-agent install is the only in-product option.
//
// Because the HTTP status is a committed 200 once the first byte is sent, the
// terminal outcome travels in-band as a marker on the last line of the stream
// (see installMarkerPrefix in internal/server/servers_routes.go). A stream that
// ends without it is a lost connection, not a success.

import { computed, onBeforeUnmount, ref } from "vue";
import type { ComputedRef, Ref } from "vue";

import { getAccessToken, getRefreshToken } from "@/shared/api/token";
import { expireSession, isStaleRefreshError, refreshSession } from "@/shared/api/http";
import { activeLocale, i18n } from "@/shared/i18n";
import { describeServerError, dockerInstallPath } from "@/features/servers/api/servers";
import { cleanLogLine } from "@/features/toolchain/composables/useInstallQueueLog";
import type { QueuedLogLine } from "@/features/toolchain/composables/useInstallQueueLog";

/** Lifecycle of one Docker-install stream. */
export type DockerInstallStatus = "idle" | "running" | "done" | "error";

/**
 * Marker prefix of the stream's final status line. Keep in sync with
 * installMarkerPrefix in internal/server/servers_routes.go.
 */
const installMarkerPrefix = "\x1e[gotham-install] ";

/**
 * Response header carrying the per-request marker nonce. The marker is only
 * honored when its nonce matches, so remote output that impersonates the
 * prefix cannot fake a terminal status. Keep in sync with installNonceHeader
 * in internal/server/servers_routes.go.
 */
const installNonceHeader = "X-Install-Nonce";

/** Locale key for a stream that ended without a terminal marker. */
const streamLostKey = "servers.wizard.dockerInstallLost";

/** Maximum rendered lines before the oldest are dropped. */
const maxLines = 2000;
/** Extra lines kept past the cap before trimming, so trimming is amortized. */
const trimSlack = 200;
/** Maximum unterminated output buffered before it is flushed as one line. */
const maxBufferChars = 64 * 1024;

/** Terminal marker payload written by the control plane. */
interface InstallMarker {
  status?: string;
  nonce?: string;
  exit_code?: number;
  message?: string;
}

/** A marker line held back until the stream proves it was the final line. */
interface PendingMarker {
  /** The raw line, kept so a demoted marker still renders as ordinary text. */
  raw: string;
  marker: InstallMarker;
}

export interface DockerInstallSource {
  /** Server being installed on; empty disables the stream. */
  serverId: Ref<string>;
  /** Transient passphrase for a protected key, sent per run and never stored. */
  passphrase: Ref<string>;
  /** Operator consent to trust an unpinned host key, required for password auth. */
  trustHostKey: Ref<boolean>;
}

export interface DockerInstallState {
  status: Ref<DockerInstallStatus>;
  lines: Ref<QueuedLogLine[]>;
  /** error renders the retained failure in the active locale, "" while healthy. */
  error: ComputedRef<string>;
  start: () => Promise<void>;
  stop: () => void;
  reset: () => void;
}

export function useDockerInstall(source: DockerInstallSource): DockerInstallState {
  const status = ref<DockerInstallStatus>("idle");
  const lines = ref<QueuedLogLine[]>([]);
  /**
   * failure retains the raw last failure. A plain string is display text (a
   * marker message or the stream-lost key) rendered verbatim; an object goes
   * through the localized describeServerError.
   */
  const failure = ref<unknown>(null);
  const error = computed<string>(() => {
    // Tracks the locale so a language switch re-renders a retained failure.
    void activeLocale.value;
    const retained = failure.value;
    if (retained === null || retained === undefined || retained === "") {
      return "";
    }
    if (typeof retained === "string") {
      return retained === streamLostKey ? String(i18n.global.t(streamLostKey)) : retained;
    }
    return describeServerError(retained);
  });

  let controller: AbortController | null = null;
  let nextSeq = 0;
  let terminal: "ok" | "fail" | null = null;
  /** nonce is the response's marker nonce; a marker without it is plain text. */
  let nonce = "";
  /** pending holds the last marker-looking line until the stream ends. */
  let pending: PendingMarker | null = null;

  /** reset clears the buffer and the failure, returning to idle. */
  function reset(): void {
    stop();
    lines.value = [];
    failure.value = null;
    status.value = "idle";
    terminal = null;
    nonce = "";
    pending = null;
  }

  function stop(): void {
    if (controller === null) {
      return;
    }
    controller.abort();
    controller = null;
    // A stopped run is not "running" any more: leaving the flag set would make
    // start() refuse the next attempt after the modal is reopened.
    if (status.value === "running") {
      status.value = "idle";
    }
  }

  /** appendLine adds one stream line, capped and ANSI-cleaned. */
  function appendLine(raw: string): void {
    // A pty-backed remote maps newlines to CRLF; the trailing CR is framing.
    const text = raw.endsWith("\r") ? raw.slice(0, -1) : raw;
    lines.value.push({ seq: ++nextSeq, text: cleanLogLine(text) });
    if (lines.value.length > maxLines + trimSlack) {
      // Replace (not splice) so the reactive array is swapped once per trim.
      lines.value = lines.value.slice(-maxLines);
    }
  }

  /** handleLine routes one completed output line to the marker or the log. */
  function handleLine(raw: string): void {
    const line = raw.endsWith("\r") ? raw.slice(0, -1) : raw;
    if (line.startsWith(installMarkerPrefix)) {
      // Hold it: only the FINAL line of the stream is a terminal marker, so a
      // line that only looks like one must not decide the outcome yet.
      const marker = parseMarker(line.slice(installMarkerPrefix.length));
      if (marker === null) {
        appendLine(line); // not JSON: ordinary remote text
        return;
      }
      pending = { raw: line, marker };
      return;
    }
    if (pending !== null) {
      // A further non-blank line proves the held marker was not terminal.
      if (line.trim() !== "") {
        demotePending();
      } else {
        return; // a blank separator does not disown the marker
      }
    }
    appendLine(line);
  }

  /** parseMarker decodes a marker payload, or null when it is not one. */
  function parseMarker(payload: string): InstallMarker | null {
    try {
      const parsed = JSON.parse(payload) as InstallMarker;
      return parsed !== null && typeof parsed === "object" ? parsed : null;
    } catch {
      return null;
    }
  }

  /** demotePending renders a held marker line as ordinary remote text. */
  function demotePending(): void {
    if (pending === null) {
      return;
    }
    appendLine(pending.raw);
    pending = null;
  }

  /**
   * finalizeTerminal accepts the held line as the terminal marker only when it
   * was the last line and carries the response nonce. Anything else is remote
   * text, and the absent marker leaves the stream outcome unknown.
   */
  function finalizeTerminal(): void {
    const held = pending;
    pending = null;
    if (held === null) {
      return;
    }
    if (nonce === "" || held.marker.nonce !== nonce) {
      appendLine(held.raw); // impersonated marker: ordinary text
      return;
    }
    if (held.marker.status === "fail") {
      terminal = "fail";
      failure.value =
        held.marker.message && held.marker.message !== "" ? held.marker.message : streamLostKey;
      return;
    }
    terminal = "ok";
    failure.value = null;
  }

  /** start runs one install and consumes its streamed output. */
  async function start(): Promise<void> {
    const serverId = source.serverId.value;
    if (serverId === "" || status.value === "running") {
      return;
    }
    stop();
    lines.value = [];
    failure.value = null;
    nextSeq = 0;
    terminal = null;
    nonce = "";
    pending = null;
    status.value = "running";

    const abort = new AbortController();
    controller = abort;
    try {
      const response = await openStream(serverId, abort.signal);
      if (!response.ok || response.body === null) {
        failure.value = await toApiError(response);
        status.value = "error";
        return;
      }
      nonce = response.headers?.get?.(installNonceHeader) ?? "";
      await readBody(response.body, abort);
      if (abort.signal.aborted || controller !== abort) {
        return;
      }
      // Accept the held line as the terminal marker only here: the stream has
      // ended, so it really was the last line.
      finalizeTerminal();
      // No valid marker means the stream was truncated: the outcome is unknown,
      // so it is a failure rather than a silent success.
      if (terminal === "ok") {
        status.value = "done";
      } else {
        if (terminal === null) {
          failure.value = streamLostKey;
        }
        status.value = "error";
      }
    } catch (err) {
      if (!abort.signal.aborted && controller === abort) {
        failure.value = err;
        status.value = "error";
      }
    } finally {
      if (controller === abort) {
        controller = null;
      }
    }
  }

  /**
   * openStream issues the install request, refreshing the session once on 401.
   * The reader cannot use the axios instance (it consumes a chunked body), so it
   * wires the shared refresh flow itself.
   */
  async function openStream(serverId: string, signal: AbortSignal): Promise<Response> {
    const body: Record<string, unknown> = {};
    if (source.passphrase.value) {
      body.passphrase = source.passphrase.value;
    }
    if (source.trustHostKey.value) {
      body.trust_host_key = true;
    }
    // The token is read per attempt, so a retry after refreshSession() uses the
    // refreshed token rather than the stale one that earned the 401.
    const request = (): Promise<Response> => {
      const token = getAccessToken();
      return fetch(dockerInstallPath(serverId), {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify(body),
        signal,
      });
    };

    let response = await request();
    if (response.status !== 401) {
      return response;
    }
    const tokenBeforeRefresh = getRefreshToken();
    try {
      await refreshSession();
    } catch (refreshError) {
      if (!isStaleRefreshError(refreshError) && getRefreshToken() === tokenBeforeRefresh) {
        expireSession();
        throw new Error("session expired", { cause: refreshError });
      }
    }
    response = await request();
    return response;
  }

  /** readBody consumes the chunked body into rendered lines. */
  async function readBody(body: ReadableStream<Uint8Array>, abort: AbortController): Promise<void> {
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
        for (const part of parts) {
          handleLine(part);
        }
        // Output without a newline (progress bars, a stuck command) must not
        // grow the buffer without bound.
        if (buffer.length > maxBufferChars) {
          appendLine(buffer);
          buffer = "";
        }
      }
      // Flush the decoder: a trailing multi-byte character may still be held.
      buffer += decoder.decode();
      if (buffer !== "") {
        handleLine(buffer);
      }
    } finally {
      if (abort.signal.aborted) {
        await reader.cancel().catch(() => undefined);
      }
      reader.releaseLock();
    }
  }

  /** toApiError maps a failed response to the shared ApiError shape. */
  async function toApiError(response: Response): Promise<unknown> {
    const text = await response.text().catch(() => "");
    let message: string;
    try {
      const parsed = JSON.parse(text) as { message?: string };
      message = parsed.message ?? "";
    } catch {
      message = text.trim();
    }
    return { status: response.status, message, cause: null };
  }

  onBeforeUnmount(stop);

  return { status, lines, error, start, stop, reset };
}
