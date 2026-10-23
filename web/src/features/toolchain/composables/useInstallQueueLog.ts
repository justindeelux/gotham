
import { onBeforeUnmount, ref, watch } from "vue";
import type { Ref } from "vue";

import { getJob } from "@/features/toolchain/api/toolchain";
import type { ToolchainLogLine } from "@/features/toolchain/api/toolchain";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";

export const MAX_JOB_LOG_LINES = 500;

export const JOB_LOG_PAGE_SIZE = 2000;

const MAX_PAGES_PER_REFRESH = 10;

export interface QueuedLogLine {
  seq: number;
  text: string;
}

export function splitLogText(log: string): string[] {
  if (log === "") {
    return [];
  }
  const lines = log.split("\n");
  if (lines.length > 0 && lines[lines.length - 1] === "") {
    lines.pop();
  }
  return lines;
}

export function cleanLogLine(raw: string): string {
  const withoutEscapes = raw
    // eslint-disable-next-line no-control-regex -- matching ESC is the point: strip ANSI CSI sequences.
    .replace(/\x1b\[[0-9;?]*[A-Za-z]/g, "")
    // eslint-disable-next-line no-control-regex -- matching ESC/BEL is the point: strip ANSI OSC sequences.
    .replace(/\x1b\][^\x07\\]*(?:\x07|\x1b\\)/g, "");
  const segments = withoutEscapes.split("\r");
  return segments[segments.length - 1] ?? "";
}

export function assignSeqs(texts: string[], nextSeq: number): QueuedLogLine[] {
  const start = nextSeq - texts.length + 1;
  return texts.map((text, index) => ({ seq: start + index, text: cleanLogLine(text) }));
}

export function mergeLogLines(
  existing: QueuedLogLine[],
  incoming: QueuedLogLine[],
  maxLines: number = MAX_JOB_LOG_LINES,
): QueuedLogLine[] {
  const seen = new Set<number>();
  for (const line of existing) {
    seen.add(line.seq);
  }
  const next = [...existing];
  for (const line of incoming) {
    if (seen.has(line.seq)) {
      continue;
    }
    seen.add(line.seq);
    next.push(line);
  }
  next.sort((a, b) => a.seq - b.seq);
  return next.length > maxLines ? next.slice(next.length - maxLines) : next;
}

export interface InstallQueueLogOptions {
  serverId: Ref<string>;
  jobId: Ref<string | null>;
  enabled: Ref<boolean>;
  active: Ref<boolean>;
  pollMs?: number;
}

export const INSTALL_QUEUE_POLL_MS = 5000;

export function useInstallQueueLog(options: InstallQueueLogOptions) {
  const store = useToolchainStore();
  const lines = ref<QueuedLogLine[]>([]);
  const cursor = ref(0);
  const high = ref(0);
  const loading = ref(false);
  const failure = ref<unknown>(null);

  const pollMs = options.pollMs ?? INSTALL_QUEUE_POLL_MS;
  const received = new Set<number>();
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let generation = 0;

  function absorb(incoming: QueuedLogLine[]): void {
    if (incoming.length === 0) {
      return;
    }
    lines.value = mergeLogLines(lines.value, incoming);
    for (const line of incoming) {
      received.add(line.seq);
      if (line.seq > high.value) {
        high.value = line.seq;
      }
    }
    while (received.has(cursor.value + 1)) {
      cursor.value += 1;
    }
  }

  function appendLine(line: ToolchainLogLine): void {
    if (line.job_id !== options.jobId.value) {
      return;
    }
    if (received.has(line.seq)) {
      return;
    }
    absorb([{ seq: line.seq, text: cleanLogLine(line.line) }]);
    if (line.seq > cursor.value + 1) {
      void loadTail();
    }
  }

  const unsubscribe = store.onJobLog(appendLine);

  function stopPoll(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  async function fetchPages(
    run: number,
    serverId: string,
    jobId: string,
    startAfter: number | undefined,
  ): Promise<{ lines: QueuedLogLine[]; nextSeq: number } | null> {
    const collected: QueuedLogLine[] = [];
    let after = startAfter;
    let nextSeq = startAfter ?? 0;
    for (let page = 0; page < MAX_PAGES_PER_REFRESH; page += 1) {
      const detail = await getJob(serverId, jobId, after);
      if (run !== generation || options.jobId.value !== jobId) {
        return null;
      }
      const texts = splitLogText(detail.log);
      store.applyJob(detail.job);
      if (texts.length === 0) {
        if (detail.next_seq > nextSeq) {
          nextSeq = detail.next_seq;
        }
        break;
      }
      collected.push(...assignSeqs(texts, detail.next_seq));
      after = detail.next_seq;
      nextSeq = detail.next_seq;
      if (texts.length < JOB_LOG_PAGE_SIZE) {
        break;
      }
    }
    return { lines: collected, nextSeq };
  }

  async function loadTail(): Promise<void> {
    const serverId = options.serverId.value;
    const jobId = options.jobId.value;
    if (serverId === "" || jobId === null) {
      return;
    }
    const run = generation;
    try {
      const page = await fetchPages(run, serverId, jobId, cursor.value);
      if (page === null || run !== generation || options.jobId.value !== jobId) {
        return;
      }
      absorb(page.lines);
    } catch {
    }
  }

  async function loadInitial(): Promise<void> {
    const serverId = options.serverId.value;
    const jobId = options.jobId.value;
    if (serverId === "" || jobId === null) {
      return;
    }
    const run = ++generation;
    stopPoll();
    lines.value = [];
    received.clear();
    cursor.value = 0;
    high.value = 0;
    failure.value = null;
    loading.value = true;
    try {
      const page = await fetchPages(run, serverId, jobId, undefined);
      if (page === null || run !== generation || options.jobId.value !== jobId) {
        return;
      }
      absorb(page.lines);
    } catch (err) {
      if (run !== generation || options.jobId.value !== jobId) {
        return;
      }
      failure.value = err;
    } finally {
      if (run === generation && options.jobId.value === jobId) {
        loading.value = false;
      }
    }
    ensurePoll();
  }

  function ensurePoll(): void {
    stopPoll();
    if (pollMs <= 0 || !options.enabled.value || !options.active.value) {
      return;
    }
    if (options.serverId.value === "" || options.jobId.value === null) {
      return;
    }
    pollTimer = setInterval(() => {
      if (!options.active.value || !options.enabled.value) {
        stopPoll();
        return;
      }
      void loadTail();
    }, pollMs);
  }

  async function reload(): Promise<void> {
    if (lines.value.length === 0 && !loading.value && cursor.value === 0) {
      await loadInitial();
      return;
    }
    await loadTail();
  }

  watch(
    [() => options.serverId.value, () => options.jobId.value, () => options.enabled.value],
    ([serverId, jobId, enabled]) => {
      if (enabled && serverId !== "" && jobId !== null) {
        void loadInitial();
      } else {
        generation += 1;
        stopPoll();
      }
    },
    { immediate: true },
  );

  watch(
    () => options.active.value,
    (active, wasActive) => {
      if (!options.enabled.value || options.jobId.value === null) {
        return;
      }
      if (active) {
        ensurePoll();
        return;
      }
      stopPoll();
      if (wasActive) {
        void loadTail();
      }
    },
  );

  onBeforeUnmount(() => {
    stopPoll();
    unsubscribe();
  });

  return { lines, cursor, high, loading, failure, reload };
}
