// Install progress modal queue log (JUS-178): seq de-dupe, `?after`
// cursor loads, realtime merge, and the retry/cancel store actions.
// The realtime client and the HTTP layer are mocked; socket frames are
// driven manually through the captured client options.
import { createPinia, setActivePinia } from "pinia";
import { computed, defineComponent, ref } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useWebSocket } from "@/shared/composables/useWebSocket";
import type {
  UseWebSocketOptions,
  WebSocketMessage,
} from "@/shared/composables/useWebSocket";

vi.mock("@/shared/composables/useWebSocket", () => ({
  useWebSocket: vi.fn(),
}));

vi.mock("@/features/toolchain/api/toolchain", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/toolchain/api/toolchain")>();
  return {
    ...actual,
    listTools: vi.fn(),
    listJobs: vi.fn(),
    getJob: vi.fn(),
    enqueueJobs: vi.fn(),
    cancelJob: vi.fn(),
  };
});

import {
  cancelJob,
  enqueueJobs,
  getJob,
  listJobs,
  listTools,
} from "@/features/toolchain/api/toolchain";
import type { ToolchainJob } from "@/features/toolchain/api/toolchain";
import {
  assignSeqs,
  mergeLogLines,
  splitLogText,
  useInstallQueueLog,
} from "@/features/toolchain/composables/useInstallQueueLog";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";

const useWebSocketMock = vi.mocked(useWebSocket);
const listToolsMock = vi.mocked(listTools);
const listJobsMock = vi.mocked(listJobs);
const getJobMock = vi.mocked(getJob);
const enqueueJobsMock = vi.mocked(enqueueJobs);
const cancelJobMock = vi.mocked(cancelJob);

const socketStub = {
  subscribe: vi.fn(),
  unsubscribe: vi.fn(),
  close: vi.fn(),
};

let capturedOptions: UseWebSocketOptions | null = null;

function job(overrides: Partial<ToolchainJob> = {}): ToolchainJob {
  return {
    id: "job-1",
    server_id: "srv-1",
    tool_id: "docker",
    action: "install",
    status: "running",
    created_at: "2026-10-16T12:00:00Z",
    ...overrides,
  };
}

/** logFrame wraps one output line as a realtime hub frame. */
function logFrame(jobId: string, seq: number, line: string): string {
  return JSON.stringify({
    channel: "logs:srv-1:toolchain",
    type: "toolchain_log",
    data: JSON.stringify({ job_id: jobId, seq, stream: "stdout", line }),
  });
}

/** socketMessage builds the client message the store's handler reads. */
function socketMessage(raw: string): WebSocketMessage {
  return {
    raw,
    payload: null,
    channel: "logs:srv-1:toolchain",
    kind: "data",
    receivedAt: Date.now(),
  };
}

/** emitLogLine drives one realtime line through the store fan-out. */
function emitLogLine(jobId: string, seq: number, line: string): void {
  capturedOptions?.onMessage?.(socketMessage(logFrame(jobId, seq, line)));
}

const Harness = defineComponent({
  template: "<div />",
  setup() {
    const store = useToolchainStore();
    const jobId = ref<string | null>("job-1");
    const enabled = ref(true);
    const active = ref(true);
    const tailed = useInstallQueueLog({
      serverId: computed(() => store.serverId),
      jobId,
      enabled,
      active,
      pollMs: 0,
    });
    return { store, jobId, enabled, active, ...tailed };
  },
});

beforeEach(() => {
  setActivePinia(createPinia());
  capturedOptions = null;
  socketStub.subscribe.mockClear();
  socketStub.unsubscribe.mockClear();
  socketStub.close.mockClear();
  useWebSocketMock.mockReset();
  useWebSocketMock.mockImplementation((options: UseWebSocketOptions) => {
    capturedOptions = options;
    return socketStub as unknown as ReturnType<typeof useWebSocket>;
  });
  listToolsMock.mockReset().mockResolvedValue([]);
  listJobsMock.mockReset().mockResolvedValue([]);
  getJobMock.mockReset();
  enqueueJobsMock.mockReset();
  cancelJobMock.mockReset();
});

describe("splitLogText", () => {
  it("returns no lines for an empty log", () => {
    expect(splitLogText("")).toEqual([]);
  });

  it("drops the trailing framing newline but keeps interior blanks", () => {
    expect(splitLogText("a\n\nb\n")).toEqual(["a", "", "b"]);
  });
});

describe("assignSeqs", () => {
  it("numbers a page so the last line lands on next_seq", () => {
    expect(assignSeqs(["a", "b"], 7)).toEqual([
      { seq: 6, text: "a" },
      { seq: 7, text: "b" },
    ]);
  });
});

describe("mergeLogLines", () => {
  it("de-dupes by seq, sorts late frames into place, and caps the buffer", () => {
    const base = [
      { seq: 1, text: "a" },
      { seq: 2, text: "b" },
    ];
    const merged = mergeLogLines(base, [
      { seq: 2, text: "b-redelivery" },
      { seq: 4, text: "d" },
      { seq: 3, text: "c" },
    ]);
    expect(merged).toEqual([
      { seq: 1, text: "a" },
      { seq: 2, text: "b" },
      { seq: 3, text: "c" },
      { seq: 4, text: "d" },
    ]);
    const capped = mergeLogLines(merged, [{ seq: 5, text: "e" }], 3);
    expect(capped.map((line) => line.seq)).toEqual([3, 4, 5]);
  });
});

describe("useInstallQueueLog", () => {
  it("loads the persisted log, merges realtime lines, and de-dupes replays", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job());
    getJobMock.mockResolvedValue({
      job: job(),
      log: "pulling\ninstalling\n",
      next_seq: 2,
    });

    const wrapper = mount(Harness);
    await flushPromises();

    expect(wrapper.vm.lines.map((line) => line.seq)).toEqual([1, 2]);
    expect(wrapper.vm.maxSeq).toBe(2);

    // A redelivered frame (same seq) never renders twice; a late frame
    // sorts into place and advances the cursor.
    emitLogLine("job-1", 2, "installing");
    emitLogLine("job-1", 4, "fourth");
    emitLogLine("job-1", 3, "third");
    // Another job's lines never leak into this buffer.
    emitLogLine("job-2", 5, "elsewhere");
    await flushPromises();

    expect(wrapper.vm.lines.map((line) => line.seq)).toEqual([1, 2, 3, 4]);
    expect(wrapper.vm.lines.map((line) => line.text)).toEqual([
      "pulling",
      "installing",
      "third",
      "fourth",
    ]);
    expect(wrapper.vm.maxSeq).toBe(4);
    wrapper.unmount();
  });

  it("tails from the cursor and ignores an already-rendered page", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job());
    getJobMock.mockImplementation(async (_serverId: string, _jobId: string, after?: number) => {
      if (after !== undefined && after > 0) {
        return { job: job(), log: "done\n", next_seq: after + 1 };
      }
      return { job: job(), log: "pulling\n", next_seq: 1 };
    });

    const wrapper = mount(Harness);
    await flushPromises();
    expect(wrapper.vm.lines.map((line) => line.seq)).toEqual([1]);

    await wrapper.vm.reload();
    expect(getJobMock).toHaveBeenLastCalledWith("srv-1", "job-1", 1);
    expect(wrapper.vm.lines.map((line) => line.seq)).toEqual([1, 2]);
    expect(wrapper.vm.maxSeq).toBe(2);

    // The cursor poll is idempotent: replaying the same page adds nothing.
    getJobMock.mockResolvedValue({ job: job(), log: "done\n", next_seq: 2 });
    await wrapper.vm.reload();
    expect(wrapper.vm.lines.map((line) => line.seq)).toEqual([1, 2]);
    wrapper.unmount();
  });

  it("surfaces a failed initial load for the retry button", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job());
    getJobMock.mockRejectedValue(new Error("offline"));

    const wrapper = mount(Harness);
    await flushPromises();

    expect(wrapper.vm.failure).not.toBeNull();
    expect(wrapper.vm.lines).toEqual([]);
    wrapper.unmount();
  });
});

describe("retry/cancel actions", () => {
  it("retries a failed job by re-enqueueing its tool action", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job({ status: "failed" }));
    const fresh = job({ id: "job-2", status: "queued", created_at: "2026-10-16T12:05:00Z" });
    enqueueJobsMock.mockResolvedValue({ jobs: [fresh], skipped: [] });

    const created = await store.retry("job-1");

    expect(enqueueJobsMock).toHaveBeenCalledWith("srv-1", ["docker"], "install");
    expect(created).toEqual([fresh]);
    expect(store.jobs["job-2"]).toEqual(fresh);
  });

  it("rejects a retry for an unknown job", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    await expect(store.retry("missing")).rejects.toThrow();
    expect(enqueueJobsMock).not.toHaveBeenCalled();
  });

  it("applies the canceled job returned by the API", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job({ status: "queued" }));
    cancelJobMock.mockResolvedValue(job({ status: "canceled" }));

    await store.cancel("job-1");

    expect(cancelJobMock).toHaveBeenCalledWith("srv-1", "job-1");
    expect(store.jobs["job-1"].status).toBe("canceled");
  });
});
