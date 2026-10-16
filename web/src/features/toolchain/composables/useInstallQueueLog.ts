// Install-queue log tail for the toolchain progress modal (JUS-178).
//
// Owns one job's rendered log buffer: the persisted log is loaded through
// `GET .../jobs/{jobID}?after=` (paged, 2000 lines per call), live lines
// arrive on the store's realtime fan-out, and both sides de-dupe by `seq`.
//
// Two watermarks are tracked because realtime is lossy but the cursor must
// not be: `cursor` is the highest *contiguous* seq (every seq below it has
// rendered at least once, so `?after=cursor` never skips a line), while
// `high` is the highest seq seen at all (drives auto-scroll). A realtime
// frame past `cursor + 1` means lines were missed, so it triggers an
// immediate cursor backfill; the 5s poll (same cursor) heals anything else,
// including a disconnect the socket layer never signals to this composable.
// The buffer is capped client-side; the oldest lines are dropped first.

import { onBeforeUnmount, ref, watch } from "vue";
import type { Ref } from "vue";

import { getJob } from "@/features/toolchain/api/toolchain";
import type { ToolchainLogLine } from "@/features/toolchain/api/toolchain";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";

/** Maximum rendered lines kept per job before the oldest are dropped. */
export const MAX_JOB_LOG_LINES = 500;

/** Lines returned per `GET job` page (server contract from #325). */
export const JOB_LOG_PAGE_SIZE = 2000;

/** Safety bound on cursor pages per refresh (the server caps logs at 256 KiB). */
const MAX_PAGES_PER_REFRESH = 10;

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
 * cleanLogLine makes one raw output line safe and readable as plain text:
 * ANSI escape sequences are stripped and `\r` progress rewrites collapse to
 * their final segment. Interpolation still renders it as text only.
 */
export function cleanLogLine(raw: string): string {
  // CSI `ESC[ … letter`, plus OSC `ESC] … BEL` / `ESC\` for completeness.
  const withoutEscapes = raw
    // eslint-disable-next-line no-control-regex -- matching ESC is the point: strip ANSI CSI sequences.
    .replace(/\x1b\[[0-9;?]*[A-Za-z]/g, "")
    // eslint-disable-next-line no-control-regex -- matching ESC/BEL is the point: strip ANSI OSC sequences.
    .replace(/\x1b\][^\x07\\]*(?:\x07|\x1b\\)/g, "");
  const segments = withoutEscapes.split("\r");
  return segments[segments.length - 1] ?? "";
}

/**
 * assignSeqs numbers freshly fetched page lines so the last one lands on
 * `nextSeq`. This assumes what the API guarantees for the append-only log
 * table: sequence numbers are contiguous and each stored line holds no
 * newline (the server splits agent output on `\n` before appending), so a
 * page of N lines ending at `nextSeq` covers `nextSeq - N + 1 .. nextSeq`.
 * Realtime frames carry their own `seq` and never go through here.
 */
export function assignSeqs(texts: string[], nextSeq: number): QueuedLogLine[] {
  const start = nextSeq - texts.length + 1;
  return texts.map((text, index) => ({ seq: start + index, text: cleanLogLine(text) }));
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
 * useInstallQueueLog tails one job's log: paged cursor load on select/open,
 * live realtime appends, gap-triggered backfill, and a best-effort `?after=`
 * poll while the job runs so frames missed during a disconnect still
 * appear. Callers render `lines` as plain text (never v-html: log content
 * is untrusted agent output).
 */
export function useInstallQueueLog(options: InstallQueueLogOptions) {
  const store = useToolchainStore();
  const lines = ref<QueuedLogLine[]>([]);
  /**
   * cursor is the contiguous watermark: every seq at or below it rendered
   * at least once, so it — and never the lossy realtime high — feeds
   * `?after=`. received remembers every merged seq (including lines since
   * evicted by the cap) so contiguity survives buffer rotation.
   */
  const cursor = ref(0);
  /** high is the highest seq seen (realtime or cursor); drives auto-scroll. */
  const high = ref(0);
  const loading = ref(false);
  const failure = ref<unknown>(null);

  const pollMs = options.pollMs ?? INSTALL_QUEUE_POLL_MS;
  const received = new Set<number>();
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  /**
   * generation guards overlapping loads: selecting another job bumps it so
   * the previous job's in-flight response cannot clobber the new buffer.
   */
  let generation = 0;

  /** absorb merges lines, records their seqs, and advances both watermarks. */
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

  /** appendLine merges one realtime line; a jump past the cursor backfills. */
  function appendLine(line: ToolchainLogLine): void {
    if (line.job_id !== options.jobId.value) {
      return;
    }
    if (received.has(line.seq)) {
      return;
    }
    absorb([{ seq: line.seq, text: cleanLogLine(line.line) }]);
    // A frame past cursor + 1 proves lines were missed (disconnect, drop):
    // backfill from the contiguous cursor now instead of waiting for the poll.
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

  /**
   * fetchPages walks the `?after=` cursor until a short page arrives, so
   * logs over one 2000-line page still render fully. Returns null when a
   * newer selection superseded the walk.
   */
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
      // A page that carries a job frame keeps the queue row fresh even
      // between realtime frames.
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

  /**
   * loadTail catches up from the contiguous cursor. Failures are
   * best-effort: the rendered lines stay and the next poll retries.
   */
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
      // Best-effort: a failed poll keeps the lines rendered so far and the
      // next tick retries. Only the initial load surfaces an error state.
    }
  }

  /**
   * loadInitial (re)loads the full persisted log for the selected job —
   * used on open and on job switch. A reconnect lands here too, via the
   * `enabled` watcher, so the reopened modal shows the persisted log.
   * Frames that arrive mid-load merge into the same buffer instead of
   * being overwritten by the fetch result.
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

  /** ensurePoll runs the catch-up poll while the selected job is active. */
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

  /** reload re-tails the selected job from the cursor (manual refresh). */
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

  // A job selected while queued turns active later: start the poll then.
  // A job that turns terminal stops needing it, but gets one final tail so
  // the last lines still land when their frames were dropped.
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
