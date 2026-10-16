// Install-queue log tail for the toolchain progress modal (JUS-178).
//
// Owns one job's rendered log buffer: the persisted log is loaded through
// `GET .../jobs/{jobID}?after=` (the `next_seq` cursor), live lines arrive
// on the store's realtime fan-out, and both sides de-dupe by `seq` so a
// reconnect or a cursor poll never renders a line twice. The buffer is
// capped client-side; the oldest lines are dropped first.

import { onBeforeUnmount, ref, watch } from "vue";
import type { Ref } from "vue";

import { getJob } from "@/features/toolchain/api/toolchain";
import type { ToolchainLogLine } from "@/features/toolchain/api/toolchain";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";

/** Maximum rendered lines kept per job before the oldest are dropped. */
export const MAX_JOB_LOG_LINES = 500;

/** One rendered log line keyed by its server-assigned sequence number. */
export interface QueuedLogLine {
  seq: number;
  text: string;
}

/**
 * splitLogText splits the assembled log text of one `GET job` page into
 * lines. The API newline-terminates every line, so a trailing empty segment
 * is framing, not content.
 */
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

/**
 * assignSeqs numbers freshly fetched page lines so the last one lands on
 * `nextSeq`. The log table is append-only with contiguous sequence numbers,
 * so a page of N lines ending at `nextSeq` covers
 * `nextSeq - N + 1 .. nextSeq`.
 */
export function assignSeqs(texts: string[], nextSeq: number): QueuedLogLine[] {
  const start = nextSeq - texts.length + 1;
  return texts.map((text, index) => ({ seq: start + index, text }));
}

/**
 * mergeLogLines appends incoming lines to the buffer, de-duplicating by
 * `seq` (reconnect replays and cursor overlaps never render twice) and
 * keeping the buffer sorted and capped. Out-of-order realtime frames sort
 * into place instead of dropping a line that only arrived late.
 */
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
  /** Node the selected job belongs to (the store's current server). */
  serverId: Ref<string>;
  /** Job whose log renders, or null when nothing is selected. */
  jobId: Ref<string | null>;
  /** True while the modal is open (polling and loading stop when closed). */
  enabled: Ref<boolean>;
  /** True while the selected job is still running (gates the catch-up poll). */
  active: Ref<boolean>;
  /** Catch-up poll interval in ms; 0 disables polling (tests). */
  pollMs?: number;
}

/** Default catch-up poll while a job runs (reconnects never replay frames). */
export const INSTALL_QUEUE_POLL_MS = 5000;

/**
 * useInstallQueueLog tails one job's log: full load on select/open, live
 * realtime appends, and a best-effort `?after=` poll while the job runs so
 * frames missed during a disconnect still appear. Callers render `lines`
 * as plain text (never v-html: log content is untrusted agent output).
 */
export function useInstallQueueLog(options: InstallQueueLogOptions) {
  const store = useToolchainStore();
  const lines = ref<QueuedLogLine[]>([]);
  /** maxSeq is the highest rendered seq; the next cursor poll asks after it. */
  const maxSeq = ref(0);
  const loading = ref(false);
  const failure = ref<unknown>(null);

  const pollMs = options.pollMs ?? INSTALL_QUEUE_POLL_MS;
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  /**
   * generation guards overlapping loads: selecting another job bumps it so
   * the previous job's in-flight response cannot clobber the new buffer.
   */
  let generation = 0;

  /** appendLine merges one realtime line unless its seq already rendered. */
  function appendLine(line: ToolchainLogLine): void {
    if (line.job_id !== options.jobId.value) {
      return;
    }
    lines.value = mergeLogLines(lines.value, [{ seq: line.seq, text: line.line }]);
    if (line.seq > maxSeq.value) {
      maxSeq.value = line.seq;
    }
  }

  const unsubscribe = store.onJobLog(appendLine);

  function stopPoll(): void {
    if (pollTimer !== null) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  /**
   * loadTail catches up from the cursor (`?after=maxSeq`) and applies the
   * job frame the page carries, so the queue row stays fresh even between
   * realtime frames. Failures are best-effort: the next poll retries.
   */
  async function loadTail(): Promise<void> {
    const serverId = options.serverId.value;
    const jobId = options.jobId.value;
    if (serverId === "" || jobId === null) {
      return;
    }
    const run = generation;
    try {
      const detail = await getJob(serverId, jobId, maxSeq.value);
      if (run !== generation || options.jobId.value !== jobId) {
        return;
      }
      if (detail.log !== "") {
        lines.value = mergeLogLines(
          lines.value,
          assignSeqs(splitLogText(detail.log), detail.next_seq),
        );
      }
      if (detail.next_seq > maxSeq.value) {
        maxSeq.value = detail.next_seq;
      }
      store.applyJob(detail.job);
    } catch {
      // Best-effort: a failed poll keeps the lines rendered so far and the
      // next tick retries. Only the initial load surfaces an error state.
    }
  }

  /**
   * loadInitial (re)loads the full persisted log for the selected job —
   * used on open and on job switch. A reconnect lands here too, via the
   * `enabled` watcher, so the reopened modal shows the persisted log.
   */
  async function loadInitial(): Promise<void> {
    const serverId = options.serverId.value;
    const jobId = options.jobId.value;
    if (serverId === "" || jobId === null) {
      return;
    }
    const run = ++generation;
    stopPoll();
    lines.value = [];
    maxSeq.value = 0;
    failure.value = null;
    loading.value = true;
    try {
      const detail = await getJob(serverId, jobId);
      if (run !== generation || options.jobId.value !== jobId) {
        return;
      }
      lines.value = mergeLogLines(
        [],
        assignSeqs(splitLogText(detail.log), detail.next_seq),
      );
      maxSeq.value = detail.next_seq;
      store.applyJob(detail.job);
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
    if (pollMs > 0 && options.active.value) {
      pollTimer = setInterval(() => {
        if (!options.active.value) {
          stopPoll();
          return;
        }
        void loadTail();
      }, pollMs);
    }
  }

  /** reload re-tails the selected job from the cursor (manual refresh). */
  async function reload(): Promise<void> {
    if (lines.value.length === 0 && !loading.value) {
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

  // A job that turns terminal stops needing the catch-up poll; a job that
  // leaves the terminal state through a retry is a new job id, handled by
  // the watcher above.
  watch(
    () => options.active.value,
    (active) => {
      if (!active) {
        stopPoll();
      }
    },
  );

  onBeforeUnmount(() => {
    stopPoll();
    unsubscribe();
  });

  return { lines, maxSeq, loading, failure, reload };
}
