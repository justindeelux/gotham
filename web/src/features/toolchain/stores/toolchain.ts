import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  cancelJob,
  enqueueJobs,
  isTerminalJobStatus,
  listJobs,
  listTools,
  parseToolchainJobFrame,
  parseToolchainLogFrame,
  toolchainChannel,
} from "@/features/toolchain/api/toolchain";
import type {
  ToolchainAction,
  ToolchainJob,
  ToolchainLogLine,
  ToolchainTool,
} from "@/features/toolchain/api/toolchain";
import { getAccessToken } from "@/shared/api/token";
import { useWebSocket } from "@/shared/composables/useWebSocket";

/** Listener for one job's realtime output lines (JUS-178 modal seam). */
export type ToolchainLogListener = (_line: ToolchainLogLine) => void;

/**
 * Toolchain App Store state (JUS-177).
 *
 * Owns the per-node tool catalog, the job queue map (updated live from the
 * `logs:{serverID}:toolchain` room), and the enqueue/cancel actions. The
 * install-progress modal (JUS-178) builds on this store: it renders from
 * `jobs`, tails a job via `onJobLog`, and opens via `openQueueModal`.
 *
 * Socket lifecycle: the page calls `subscribe` on mount / node switch and
 * `disconnect` on unmount, so leaving the route releases the socket and the
 * channel instead of buffering log lines in the background. A consumer that
 * outlives the page (JUS-178's modal) re-establishes the subscription by
 * calling `subscribe` itself.
 */
export const useToolchainStore = defineStore("toolchain", () => {
  const serverId = ref<string>("");
  const tools = ref<ToolchainTool[]>([]);
  const jobs = ref<Record<string, ToolchainJob>>({});
  const loading = ref(false);
  const jobsLoading = ref(false);
  const failure = ref<unknown>(null);

  /**
   * queueModalOpen is the JUS-178 hook point: this screen flips it (via
   * {@link openQueueModal}) instead of rendering a modal, so the progress
   * modal owns its own markup and subscribes to the same store.
   * `queueModalJobId` optionally names the job the modal should focus.
   */
  const queueModalOpen = ref(false);
  const queueModalJobId = ref<string | null>(null);
  /** openQueueModal signals JUS-178's modal to open (no modal lives here). */
  function openQueueModal(jobId?: string): void {
    queueModalJobId.value = jobId ?? null;
    queueModalOpen.value = true;
  }
  /** closeQueueModal clears the JUS-178 hook flag. */
  function closeQueueModal(): void {
    queueModalOpen.value = false;
    queueModalJobId.value = null;
  }

  const logListeners = new Set<ToolchainLogListener>();

  /**
   * onJobLog subscribes to realtime output lines; returns an unsubscribe.
   * Realtime is best-effort — GET job detail stays the source of truth.
   */
  function onJobLog(listener: ToolchainLogListener): () => void {
    logListeners.add(listener);
    return () => {
      logListeners.delete(listener);
    };
  }

  /** jobsList is the queue newest first. */
  const jobsList = computed<ToolchainJob[]>(() =>
    Object.values(jobs.value).sort((a, b) =>
      a.created_at < b.created_at ? 1 : a.created_at > b.created_at ? -1 : 0,
    ),
  );

  /** activeJobs holds the non-terminal queue entries. */
  const activeJobs = computed<ToolchainJob[]>(() =>
    jobsList.value.filter((job) => !isTerminalJobStatus(job.status)),
  );

  /**
   * jobForTool reports the latest job for one tool (any status), or null
   * when the tool was never queued — the per-row queue badge reads this.
   */
  function jobForTool(toolId: string): ToolchainJob | null {
    let latest: ToolchainJob | null = null;
    for (const job of Object.values(jobs.value)) {
      if (job.tool_id !== toolId) {
        continue;
      }
      if (latest === null || job.created_at >= latest.created_at) {
        latest = job;
      }
    }
    return latest;
  }

  /**
   * applyJob upserts one job from REST or a realtime frame. A delayed frame
   * must never downgrade a terminal state the REST read already reported.
   */
  function applyJob(job: ToolchainJob): void {
    const known = jobs.value[job.id];
    if (known && isTerminalJobStatus(known.status) && !isTerminalJobStatus(job.status)) {
      return;
    }
    jobs.value = { ...jobs.value, [job.id]: job };
  }

  let socket: ReturnType<typeof useWebSocket> | null = null;
  let subscribedServer = "";
  /** True once the socket connected at least once (reconnects resync below). */
  let connectedOnce = false;
  /** Monotonic refresh token: only the latest request owns loading/failure. */
  let refreshSeq = 0;

  function subscribeChannel(next: string): void {
    if (!socket || next === subscribedServer) {
      return;
    }
    if (subscribedServer !== "") {
      socket.unsubscribe(toolchainChannel(subscribedServer));
    }
    subscribedServer = next;
    if (next !== "") {
      socket.subscribe(toolchainChannel(next));
    }
  }

  function ensureSocket(): void {
    if (socket) {
      return;
    }
    socket = useWebSocket({
      url: "/api/v1/ws",
      token: getAccessToken,
      autoConnect: true,
      onMessage: (message) => {
        if (message.channel !== toolchainChannel(subscribedServer)) {
          return;
        }
        // REST is the source of truth: a denied subscription means live
        // frames stop, so resync the queue instead of going stale.
        if (message.kind === "denied") {
          void refresh().catch(() => {});
          return;
        }
        const job = parseToolchainJobFrame(message.raw);
        if (job) {
          applyJob(job);
          return;
        }
        const line = parseToolchainLogFrame(message.raw);
        if (line) {
          for (const listener of logListeners) {
            listener(line);
          }
        }
      },
      onStatusChange: (status) => {
        if (status !== "open") {
          return;
        }
        // Missed frames during a disconnect are never replayed, so resync
        // the queue on every reconnect (not on the first connect, which the
        // subscribe path already loads).
        if (connectedOnce) {
          void refresh().catch(() => {});
          return;
        }
        connectedOnce = true;
      },
    });
    subscribeChannel(serverId.value);
  }

  /**
   * subscribe switches the realtime subscription (and data) to one node.
   * Call it whenever the node selector changes; repeat calls are cheap.
   */
  async function subscribe(nextServerId: string): Promise<void> {
    serverId.value = nextServerId;
    tools.value = [];
    jobs.value = {};
    failure.value = null;
    ensureSocket();
    subscribeChannel(nextServerId);
    if (nextServerId === "") {
      return;
    }
    await refresh();
  }

  /** refresh reloads the catalog and the queue for the current node. */
  async function refresh(): Promise<void> {
    const id = serverId.value;
    if (id === "") {
      return;
    }
    const token = ++refreshSeq;
    const owns = (): boolean => serverId.value === id && token === refreshSeq;
    loading.value = true;
    jobsLoading.value = true;
    failure.value = null;
    try {
      const [fetchedTools, fetchedJobs] = await Promise.all([
        listTools(id),
        listJobs(id),
      ]);
      // A node switch (or a newer refresh) racing the fetch must not
      // clobber the newer state — neither its data nor its error.
      if (!owns()) {
        return;
      }
      tools.value = fetchedTools;
      const next: Record<string, ToolchainJob> = {};
      for (const job of fetchedJobs) {
        next[job.id] = job;
      }
      jobs.value = next;
    } catch (err) {
      if (!owns()) {
        return;
      }
      failure.value = err;
      throw err;
    } finally {
      // Only the latest request clears the flags: an older request must
      // not stop the spinner a newer request still needs.
      if (owns()) {
        loading.value = false;
        jobsLoading.value = false;
      }
    }
  }

  /**
   * disconnect releases the socket and the channel subscription. Call it
   * when the last consumer unmounts (route leave) so log lines stop flowing
   * into the shared client's buffer in the background.
   */
  function disconnect(): void {
    if (socket) {
      if (subscribedServer !== "") {
        socket.unsubscribe(toolchainChannel(subscribedServer));
      }
      socket.close();
      socket = null;
    }
    subscribedServer = "";
    connectedOnce = false;
  }

  /** enqueue queues one job per tool id and merges the created jobs. */
  async function enqueue(toolIds: string[], action: ToolchainAction): Promise<string[]> {
    const result = await enqueueJobs(serverId.value, toolIds, action);
    for (const job of result.jobs) {
      applyJob(job);
    }
    return result.skipped;
  }

  /** cancel cancels a queued job (running/terminal jobs are rejected). */
  async function cancel(jobId: string): Promise<void> {
    const job = await cancelJob(serverId.value, jobId);
    applyJob(job);
  }

  /**
   * retry re-enqueues a finished job's tool action as a fresh job (the API
   * has no retry endpoint: a retry is one new queued job). Returns the
   * created jobs so the modal can focus the new attempt.
   */
  async function retry(jobId: string): Promise<ToolchainJob[]> {
    const job = jobs.value[jobId];
    if (!job) {
      throw new Error(`unknown toolchain job: ${jobId}`);
    }
    const result = await enqueueJobs(serverId.value, [job.tool_id], job.action);
    for (const created of result.jobs) {
      applyJob(created);
    }
    return result.jobs;
  }

  /** reset clears node state and closes the socket; used by sign-out. */
  function reset(): void {
    disconnect();
    serverId.value = "";
    tools.value = [];
    jobs.value = {};
    failure.value = null;
    queueModalOpen.value = false;
    queueModalJobId.value = null;
    logListeners.clear();
  }

  return {
    serverId,
    tools,
    jobs,
    jobsList,
    activeJobs,
    loading,
    jobsLoading,
    failure,
    queueModalOpen,
    queueModalJobId,
    openQueueModal,
    closeQueueModal,
    onJobLog,
    jobForTool,
    applyJob,
    subscribe,
    refresh,
    disconnect,
    enqueue,
    cancel,
    retry,
    reset,
  };
});
