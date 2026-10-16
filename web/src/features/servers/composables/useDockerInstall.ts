// Docker-install log stream for the Add-server wizard (JUS-187).
//
// The control plane runs the allow-listed package-manager install over the
// wizard's SSH session and streams the combined output as a plain-text chunked
// response (POST /servers/{id}/docker-install), so this reader consumes the
// response body directly with `fetch` (axios cannot stream a browser body).
// The node's agent is not installed at this point, so the App Store path is
// not reachable yet — this pre-agent install is the only in-product option.

import { onBeforeUnmount, ref } from "vue";
import type { Ref } from "vue";

import { getAccessToken, getRefreshToken } from "@/shared/api/token";
import { expireSession, isStaleRefreshError, refreshSession } from "@/shared/api/http";
import { describeServerError, dockerInstallPath } from "@/features/servers/api/servers";
import { cleanLogLine } from "@/features/toolchain/composables/useInstallQueueLog";
import type { QueuedLogLine } from "@/features/toolchain/composables/useInstallQueueLog";

/** Lifecycle of one Docker-install stream. */
export type DockerInstallStatus = "idle" | "running" | "done" | "error";

/** Maximum rendered lines before the oldest are dropped. */
const maxLines = 2000;

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
  error: Ref<string>;
  start: () => Promise<void>;
  stop: () => void;
  reset: () => void;
}

export function useDockerInstall(source: DockerInstallSource): DockerInstallState {
  const status = ref<DockerInstallStatus>("idle");
  const lines = ref<QueuedLogLine[]>([]);
  const error = ref<string>("");

  let controller: AbortController | null = null;
  let nextSeq = 0;

  /** reset clears the buffer and the failure, returning to idle. */
  function reset(): void {
    stop();
    lines.value = [];
    error.value = "";
    status.value = "idle";
  }

  function stop(): void {
    if (controller !== null) {
      controller.abort();
      controller = null;
    }
  }

  /** appendLine adds one stream line, capped and ANSI-cleaned. */
  function appendLine(text: string): void {
    lines.value.push({ seq: ++nextSeq, text: cleanLogLine(text) });
    if (lines.value.length > maxLines) {
      lines.value.splice(0, lines.value.length - maxLines);
    }
  }

  /** start runs one install and consumes its streamed output. */
  async function start(): Promise<void> {
    const serverId = source.serverId.value;
    if (serverId === "" || status.value === "running") {
      return;
    }
    stop();
    lines.value = [];
    error.value = "";
    nextSeq = 0;
    status.value = "running";

    const abort = new AbortController();
    controller = abort;
    try {
      const response = await openStream(serverId, abort.signal);
      if (!response.ok || response.body === null) {
        error.value = describeServerError(await toApiError(response));
        status.value = "error";
        return;
      }
      await readBody(response.body, abort);
      if (!abort.signal.aborted && controller === abort) {
        status.value = "done";
      }
    } catch (err) {
      if (!abort.signal.aborted && controller === abort) {
        error.value = describeServerError(err);
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
    const request = (): Promise<Response> =>
      fetch(dockerInstallPath(serverId), {
        method: "POST",
        headers: {
          Authorization: `Bearer ${getAccessToken() ?? ""}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify(body),
        signal,
      });

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
