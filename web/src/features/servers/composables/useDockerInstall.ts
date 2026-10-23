
import { computed, onBeforeUnmount, ref } from "vue";
import type { ComputedRef, Ref } from "vue";

import { getAccessToken, getRefreshToken } from "@/shared/api/token";
import { expireSession, isStaleRefreshError, refreshSession } from "@/shared/api/http";
import { activeLocale, i18n } from "@/shared/i18n";
import { describeServerError, dockerInstallPath } from "@/features/servers/api/servers";
import { cleanLogLine } from "@/features/toolchain/composables/useInstallQueueLog";
import type { QueuedLogLine } from "@/features/toolchain/composables/useInstallQueueLog";

export type DockerInstallStatus = "idle" | "running" | "done" | "error";

const installMarkerPrefix = "\x1e[gotham-install] ";

const installNonceHeader = "X-Install-Nonce";

const streamLostKey = "servers.wizard.dockerInstallLost";

const maxLines = 2000;
const trimSlack = 200;
const maxBufferChars = 64 * 1024;

interface InstallMarker {
  status?: string;
  nonce?: string;
  exit_code?: number;
  message?: string;
}

interface PendingMarker {
  raw: string;
  marker: InstallMarker;
}

export interface DockerInstallSource {
  serverId: Ref<string>;
  passphrase: Ref<string>;
  trustHostKey: Ref<boolean>;
}

export interface DockerInstallState {
  status: Ref<DockerInstallStatus>;
  lines: Ref<QueuedLogLine[]>;
  error: ComputedRef<string>;
  start: () => Promise<void>;
  stop: () => void;
  reset: () => void;
}

export function useDockerInstall(source: DockerInstallSource): DockerInstallState {
  const status = ref<DockerInstallStatus>("idle");
  const lines = ref<QueuedLogLine[]>([]);
  const failure = ref<unknown>(null);
  const error = computed<string>(() => {
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
  let nonce = "";
  let pending: PendingMarker | null = null;

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
    if (status.value === "running") {
      status.value = "idle";
    }
  }

  function appendLine(raw: string): void {
    const text = raw.endsWith("\r") ? raw.slice(0, -1) : raw;
    lines.value.push({ seq: ++nextSeq, text: cleanLogLine(text) });
    if (lines.value.length > maxLines + trimSlack) {
      lines.value = lines.value.slice(-maxLines);
    }
  }

  function handleLine(raw: string): void {
    const line = raw.endsWith("\r") ? raw.slice(0, -1) : raw;
    if (line.startsWith(installMarkerPrefix)) {
      const marker = parseMarker(line.slice(installMarkerPrefix.length));
      if (marker === null) {
        appendLine(line);
        return;
      }
      pending = { raw: line, marker };
      return;
    }
    if (pending !== null) {
      if (line.trim() !== "") {
        demotePending();
      } else {
        return;
      }
    }
    appendLine(line);
  }

  function parseMarker(payload: string): InstallMarker | null {
    try {
      const parsed = JSON.parse(payload) as InstallMarker;
      return parsed !== null && typeof parsed === "object" ? parsed : null;
    } catch {
      return null;
    }
  }

  function demotePending(): void {
    if (pending === null) {
      return;
    }
    appendLine(pending.raw);
    pending = null;
  }

  function finalizeTerminal(): void {
    const held = pending;
    pending = null;
    if (held === null) {
      return;
    }
    if (nonce === "" || held.marker.nonce !== nonce) {
      appendLine(held.raw);
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
      finalizeTerminal();
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

  async function openStream(serverId: string, signal: AbortSignal): Promise<Response> {
    const body: Record<string, unknown> = {};
    if (source.passphrase.value) {
      body.passphrase = source.passphrase.value;
    }
    if (source.trustHostKey.value) {
      body.trust_host_key = true;
    }
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
        if (buffer.length > maxBufferChars) {
          appendLine(buffer);
          buffer = "";
        }
      }
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
