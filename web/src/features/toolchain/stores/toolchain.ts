import { defineStore } from "pinia";
import { computed, ref, watch } from "vue";

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
   */
  const queueModalOpen = ref(false);
  /** openQueueModal signals JUS-178's modal to open (no modal lives here). */
  function openQueueModal(): void {
    queueModalOpen.value = true;
  }
  /** closeQueueModal clears the JUS-178 hook flag. */
  function closeQueueModal(): void {
    queueModalOpen.value = false;
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

  /** applyJob upserts one job from REST or a realtime frame. */
  function applyJob(job: ToolchainJob): void {
    jobs.value = { ...jobs.value, [job.id]: job };
  }

  let socket: ReturnType<typeof useWebSocket> | null = null;
  let subscribedServer = "";

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
    loading.value = true;
    jobsLoading.value = true;
    failure.value = null;
    try {
      const [fetchedTools, fetchedJobs] = await Promise.all([
        listTools(id),
        listJobs(id),
      ]);
      // A node switch racing the fetch must not clobber the new node.
      if (serverId.value !== id) {
        return;
      }
      tools.value = fetchedTools;
      const next: Record<string, ToolchainJob> = {};
      for (const job of fetchedJobs) {
        next[job.id] = job;
      }
      jobs.value = next;
    } catch (err) {
      failure.value = err;
      throw err;
    } finally {
      loading.value = false;
      jobsLoading.value = false;
    }
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

  /** reset clears node state; used by tests and sign-out. */
  function reset(): void {
    if (socket && subscribedServer !== "") {
      socket.unsubscribe(toolchainChannel(subscribedServer));
    }
    subscribedServer = "";
    serverId.value = "";
    tools.value = [];
    jobs.value = {};
    failure.value = null;
    queueModalOpen.value = false;
    logListeners.clear();
  }

  // Keep the channel pinned while the subscribe helper's socket lives, so a
  // reconnect re-subscribes through the shared client's remembered channels.
  watch(serverId, (next) => subscribeChannel(next));

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
    openQueueModal,
    closeQueueModal,
    onJobLog,
    jobForTool,
    applyJob,
    subscribe,
    refresh,
    enqueue,
    cancel,
    reset,
  };
});
