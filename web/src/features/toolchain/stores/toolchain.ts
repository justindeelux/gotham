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

export type ToolchainLogListener = (_line: ToolchainLogLine) => void;

export const useToolchainStore = defineStore("toolchain", () => {
  const serverId = ref<string>("");
  const tools = ref<ToolchainTool[]>([]);
  const jobs = ref<Record<string, ToolchainJob>>({});
  const loading = ref(false);
  const jobsLoading = ref(false);
  const failure = ref<unknown>(null);

  const queueModalOpen = ref(false);
  const queueModalJobId = ref<string | null>(null);
  function openQueueModal(jobId?: string): void {
    queueModalJobId.value = jobId ?? null;
    queueModalOpen.value = true;
  }
  function closeQueueModal(): void {
    queueModalOpen.value = false;
    queueModalJobId.value = null;
  }

  const logListeners = new Set<ToolchainLogListener>();

  function onJobLog(listener: ToolchainLogListener): () => void {
    logListeners.add(listener);
    return () => {
      logListeners.delete(listener);
    };
  }

  const jobsList = computed<ToolchainJob[]>(() =>
    Object.values(jobs.value).sort((a, b) =>
      a.created_at < b.created_at ? 1 : a.created_at > b.created_at ? -1 : 0,
    ),
  );

  const activeJobs = computed<ToolchainJob[]>(() =>
    jobsList.value.filter((job) => !isTerminalJobStatus(job.status)),
  );

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

  function applyJob(job: ToolchainJob): void {
    const known = jobs.value[job.id];
    if (known && isTerminalJobStatus(known.status) && !isTerminalJobStatus(job.status)) {
      return;
    }
    jobs.value = { ...jobs.value, [job.id]: job };
  }

  let socket: ReturnType<typeof useWebSocket> | null = null;
  let subscribedServer = "";
  let connectedOnce = false;
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
        if (message.kind === "denied") {
          void refresh().catch(() => {});
          return;
        }
        const job = parseToolchainJobFrame(message.raw);
        if (job) {
          applyJob(job);
          if (job.status === "succeeded") {
            void refresh().catch(() => {});
          }
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
        if (connectedOnce) {
          void refresh().catch(() => {});
          return;
        }
        connectedOnce = true;
      },
    });
    subscribeChannel(serverId.value);
  }

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
      if (owns()) {
        loading.value = false;
        jobsLoading.value = false;
      }
    }
  }

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

  async function enqueue(toolIds: string[], action: ToolchainAction): Promise<string[]> {
    const result = await enqueueJobs(serverId.value, toolIds, action);
    for (const job of result.jobs) {
      applyJob(job);
    }
    return result.skipped;
  }

  async function cancel(jobId: string): Promise<void> {
    const job = await cancelJob(serverId.value, jobId);
    applyJob(job);
  }

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
