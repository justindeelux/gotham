// Install progress modal queue log (JUS-178, fix round 1): seq de-dupe,
// contiguous-cursor gap backfill, paged cursor loads, poll lifecycle
// (queued→running start, terminal final tail), load-time frame preservation,
// ANSI cleanup, and the retry/cancel list actions.
// The realtime client and the HTTP layer are mocked; socket frames are
// driven manually through the captured client options.
/* global setTimeout: readonly, HTMLElement: readonly */
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { computed, defineComponent, h, nextTick, ref } from "vue";
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
import type { ToolchainJob, ToolchainTool } from "@/features/toolchain/api/toolchain";
import InstallJobLogPane from "@/features/toolchain/components/InstallJobLogPane.vue";
import InstallQueueList from "@/features/toolchain/components/InstallQueueList.vue";
import InstallQueueModal from "@/features/toolchain/components/InstallQueueModal.vue";
import toolchainEn from "@/features/toolchain/locales/en";
import {
  assignSeqs,
  cleanLogLine,
  JOB_LOG_PAGE_SIZE,
  mergeLogLines,
  splitLogText,
  useInstallQueueLog,
} from "@/features/toolchain/composables/useInstallQueueLog";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";
import { i18n, resetLocaleState } from "@/shared/i18n";

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

/** deferred returns a promise with its settle functions exposed. */
function deferred<T>() {
  let resolve!: (_value: T) => void;
  let reject!: (_reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const Harness = defineComponent({
  props: {
    pollMs: { type: Number, default: 0 },
    startActive: { type: Boolean, default: true },
  },
  template: "<div />",
  setup(props) {
    const store = useToolchainStore();
    const jobId = ref<string | null>("job-1");
    const enabled = ref(true);
    const active = ref(props.startActive);
    const tailed = useInstallQueueLog({
      serverId: computed(() => store.serverId),
      jobId,
      enabled,
      active,
      pollMs: props.pollMs,
    });
    return { store, jobId, enabled, active, ...tailed };
  },
});

const ListShell = defineComponent({
  template: "<div />",
  setup(_, { expose }) {
    const selected = ref<string | null>(null);
    const onSelect = (id: string | null): void => {
      selected.value = id;
    };
    expose({ selected });
    return () =>
      h(NMessageProvider, null, {
        default: () => h(InstallQueueList, { selectedId: selected.value, onSelect }),
      });
  },
});

beforeEach(() => {
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { toolchain: toolchainEn });
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

describe("cleanLogLine", () => {
  it("strips ANSI color sequences", () => {
    expect(cleanLogLine("\x1b[32mgreen\x1b[0m done")).toBe("green done");
  });

  it("collapses carriage-return progress rewrites to the final segment", () => {
    expect(cleanLogLine("50%\r100%")).toBe("100%");
  });

  it("leaves plain lines untouched", () => {
    expect(cleanLogLine("pulling image")).toBe("pulling image");
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
    expect(wrapper.vm.cursor).toBe(2);
    expect(wrapper.vm.high).toBe(2);

    // A redelivered frame (same seq) never renders twice; a late frame
    // sorts into place and advances both watermarks.
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
    expect(wrapper.vm.cursor).toBe(4);
    expect(wrapper.vm.high).toBe(4);
    wrapper.unmount();
  });

  it("backfills a gap from the contiguous cursor, not the realtime high", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job());
    getJobMock.mockImplementation(async (_serverId: string, _jobId: string, after?: number) => {
      if (after !== undefined && after > 0) {
        return { job: job(), log: "three\nfour\n", next_seq: 4 };
      }
      return { job: job(), log: "one\ntwo\n", next_seq: 2 };
    });

    const wrapper = mount(Harness);
    await flushPromises();
    expect(wrapper.vm.cursor).toBe(2);

    // Seq 5 arrives with 3-4 missing: the cursor must stay at 2 and the
    // backfill must ask after the cursor (2), not the realtime high (5),
    // or lines 3-4 would be lost for good.
    emitLogLine("job-1", 5, "five");
    await flushPromises();

    const tailCalls = getJobMock.mock.calls.filter((call) => call[2] !== undefined);
    expect(tailCalls.length).toBeGreaterThan(0);
    for (const call of tailCalls) {
      expect(call[2]).toBe(2);
    }
    expect(wrapper.vm.lines.map((line) => line.seq)).toEqual([1, 2, 3, 4, 5]);
    expect(wrapper.vm.cursor).toBe(5);
    expect(wrapper.vm.high).toBe(5);
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
    expect(wrapper.vm.cursor).toBe(2);

    // The cursor poll is idempotent: replaying the same page adds nothing.
    getJobMock.mockResolvedValue({ job: job(), log: "done\n", next_seq: 2 });
    await wrapper.vm.reload();
    expect(wrapper.vm.lines.map((line) => line.seq)).toEqual([1, 2]);
    wrapper.unmount();
  });

  it("pages through logs larger than one API page", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job());
    getJobMock.mockImplementation(async (_serverId: string, _jobId: string, after?: number) => {
      if (after !== undefined && after > 0) {
        return { job: job(), log: "last\n", next_seq: after + 1 };
      }
      const page = Array.from({ length: JOB_LOG_PAGE_SIZE }, (_, i) => `l${i + 1}`).join("\n");
      return { job: job(), log: `${page}\n`, next_seq: JOB_LOG_PAGE_SIZE };
    });

    const wrapper = mount(Harness);
    await flushPromises();

    expect(getJobMock).toHaveBeenCalledTimes(2);
    expect(getJobMock.mock.calls[1][2]).toBe(JOB_LOG_PAGE_SIZE);
    // The client cap keeps the newest lines; the cursor covers everything.
    expect(wrapper.vm.lines.length).toBeLessThanOrEqual(500);
    expect(wrapper.vm.lines[wrapper.vm.lines.length - 1]?.seq).toBe(JOB_LOG_PAGE_SIZE + 1);
    expect(wrapper.vm.cursor).toBe(JOB_LOG_PAGE_SIZE + 1);
    wrapper.unmount();
  });

  it("keeps frames that arrive while the initial load is in flight", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job());
    const pending = deferred<{ job: ToolchainJob; log: string; next_seq: number }>();
    getJobMock.mockReturnValue(pending.promise);

    const wrapper = mount(Harness);
    await flushPromises();
    expect(wrapper.vm.loading).toBe(true);

    // A realtime line newer than the page in flight lands first…
    emitLogLine("job-1", 3, "live");
    // …and the page load merges instead of overwriting it.
    pending.resolve({ job: job(), log: "a\nb\n", next_seq: 2 });
    await flushPromises();

    expect(wrapper.vm.lines.map((line) => line.seq)).toEqual([1, 2, 3]);
    expect(wrapper.vm.lines.map((line) => line.text)).toEqual(["a", "b", "live"]);
    wrapper.unmount();
  });

  it("starts the poll when a queued selection turns running", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job({ status: "queued" }));
    getJobMock.mockImplementation(async (_serverId: string, _jobId: string, after?: number) => {
      // Any defined cursor is a tail request (?after=0 included); only the
      // cursor-less call is the initial queued load.
      if (after !== undefined) {
        return { job: job({ status: "running" }), log: "tick\n", next_seq: after + 1 };
      }
      return { job: job({ status: "queued" }), log: "", next_seq: 0 };
    });

    const wrapper = mount(Harness, { props: { pollMs: 10, startActive: false } });
    await flushPromises();
    const callsWhileQueued = getJobMock.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 40));
    // Queued jobs get no catch-up poll: realtime only.
    expect(getJobMock.mock.calls.length).toBe(callsWhileQueued);

    wrapper.vm.active = true;
    await new Promise((resolve) => setTimeout(resolve, 60));
    const tailCalls = getJobMock.mock.calls.slice(callsWhileQueued);
    expect(tailCalls.length).toBeGreaterThan(0);
    // The first tail starts at the empty cursor; later tails advance it.
    expect(tailCalls[0][2]).toBe(0);
    for (const call of tailCalls) {
      expect(call[2]).not.toBeUndefined();
    }
    expect(wrapper.vm.lines.length).toBeGreaterThan(0);
    wrapper.unmount();
  });

  it("takes one final tail when the job turns terminal", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job());
    getJobMock.mockImplementation(async (_serverId: string, _jobId: string, after?: number) => {
      if (after !== undefined && after > 0) {
        return { job: job({ status: "succeeded" }), log: "flushed\n", next_seq: after + 1 };
      }
      return { job: job(), log: "work\n", next_seq: 1 };
    });

    const wrapper = mount(Harness, { props: { pollMs: 100000, startActive: true } });
    await flushPromises();
    expect(wrapper.vm.lines.map((line) => line.seq)).toEqual([1]);

    const callsBefore = getJobMock.mock.calls.length;
    wrapper.vm.active = false;
    await flushPromises();

    expect(getJobMock.mock.calls.length).toBe(callsBefore + 1);
    expect(getJobMock.mock.calls[callsBefore][2]).toBe(1);
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

describe("InstallQueueList", () => {
  it("focuses the fresh job after a successful retry", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job({ status: "failed" }));
    const fresh = job({ id: "job-2", status: "queued", created_at: "2026-10-16T12:05:00Z" });
    enqueueJobsMock.mockResolvedValue({ jobs: [fresh], skipped: [] });

    const wrapper = mount(ListShell, { global: { plugins: [i18n] } });
    const retry = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Retry"));
    expect(retry).toBeDefined();
    await retry?.trigger("click");
    await flushPromises();

    expect(wrapper.vm.selected).toBe("job-2");
    wrapper.unmount();
  });

  it("focuses the existing job when a retry is skipped as already queued", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job({ id: "job-1", status: "failed", created_at: "2026-10-16T12:00:00Z" }));
    store.applyJob(job({ id: "job-9", status: "running", created_at: "2026-10-16T12:06:00Z" }));
    enqueueJobsMock.mockResolvedValue({ jobs: [], skipped: ["docker"] });

    const wrapper = mount(ListShell, { global: { plugins: [i18n] } });
    const retries = wrapper
      .findAll("button")
      .filter((button) => button.text().includes("Retry"));
    expect(retries.length).toBeGreaterThan(0);
    await retries[0].trigger("click");
    await flushPromises();

    // The click must not be silent: the already-active job is focused.
    expect(wrapper.vm.selected).toBe("job-9");
    wrapper.unmount();
  });

  it("resyncs the row when cancel hits a 409 race", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job({ status: "queued" }));
    cancelJobMock.mockRejectedValue({ status: 409, message: "not queued", cause: null });
    getJobMock.mockResolvedValue({ job: job({ status: "running" }), log: "", next_seq: 0 });

    const wrapper = mount(ListShell, { global: { plugins: [i18n] } });
    const cancel = wrapper
      .findAll("button")
      .find((button) => button.text() === "Cancel");
    expect(cancel).toBeDefined();
    await cancel?.trigger("click");
    await flushPromises();

    expect(store.jobs["job-1"].status).toBe("running");
    wrapper.unmount();
  });
});

describe("git update failure", () => {
  it("shows the failed status and repository guidance in the queue modal", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    const failed = job({ tool_id: "git", status: "failed" });
    const guidance = "git 2.34.1 detected after install/update; >= 2.37 required — " +
      "the distro repository may not provide a newer version; add a newer repository / PPA";
    store.applyJob(failed);
    getJobMock.mockResolvedValue({ job: failed, log: guidance + "\n", next_seq: 1 });
    store.openQueueModal("job-1");
    const shell = defineComponent({
      setup: () => () => h(NMessageProvider, null, { default: () => h(InstallQueueModal) }),
    });
    const wrapper = mount(shell, {
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();
    expect(wrapper.text()).toContain("Failed");
    expect(wrapper.find(".log-pane").text()).toContain(guidance);
    expect(wrapper.text()).not.toContain("Succeeded");
    wrapper.unmount();
  });
});

describe("InstallQueueModal selection (JUS-184)", () => {
  function catalogTool(id: string, name: string): ToolchainTool {
    return {
      id,
      name,
      installed: false,
      installable: true,
      uninstallable: false,
      needs_root: false,
      version_ok: true,
    };
  }

  function mountModal() {
    const shell = defineComponent({
      setup: () => () => h(NMessageProvider, null, { default: () => h(InstallQueueModal) }),
    });
    return mount(shell, {
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
  }

  /** selectedToolName reads the highlighted queue row's tool name. */
  function selectedToolName(wrapper: ReturnType<typeof mount>): string {
    return wrapper.find(".queue-row.selected .queue-tool").text();
  }

  function selectRow(wrapper: ReturnType<typeof mount>, toolName: string) {
    const button = wrapper
      .findAll(".queue-select")
      .find((entry) => entry.attributes("aria-label") === toolName);
    expect(button).toBeDefined();
    return button?.trigger("click");
  }

  it("moves from a finished job to a newly enqueued running job", async () => {
    listToolsMock.mockResolvedValue([catalogTool("git", "Git"), catalogTool("railpack", "Railpack")]);
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    const finished = job({ id: "job-git", tool_id: "git", status: "succeeded" });
    store.applyJob(finished);
    getJobMock.mockResolvedValue({ job: finished, log: "done\n", next_seq: 1 });

    store.openQueueModal();
    const wrapper = mountModal();
    await flushPromises();
    expect(selectedToolName(wrapper)).toBe("Git");

    store.applyJob(
      job({ id: "job-rail", tool_id: "railpack", status: "running", created_at: "2026-10-16T12:05:00Z" }),
    );
    await flushPromises();
    expect(selectedToolName(wrapper)).toBe("Railpack");
    wrapper.unmount();
  });

  it("lands on the running job after close and reopen (stale git repro)", async () => {
    listToolsMock.mockResolvedValue([catalogTool("git", "Git"), catalogTool("railpack", "Railpack")]);
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    const finished = job({ id: "job-git", tool_id: "git", status: "succeeded" });
    store.applyJob(finished);
    getJobMock.mockResolvedValue({ job: finished, log: "done\n", next_seq: 1 });

    store.openQueueModal();
    const wrapper = mountModal();
    await flushPromises();
    expect(selectedToolName(wrapper)).toBe("Git");

    store.closeQueueModal();
    await flushPromises();
    store.applyJob(
      job({ id: "job-rail", tool_id: "railpack", status: "running", created_at: "2026-10-16T12:05:00Z" }),
    );
    await flushPromises();
    store.openQueueModal();
    await flushPromises();
    expect(selectedToolName(wrapper)).toBe("Railpack");
    wrapper.unmount();
  });

  it("keeps a manual pick of a running job when a newer job is queued", async () => {
    listToolsMock.mockResolvedValue([
      catalogTool("git", "Git"),
      catalogTool("railpack", "Railpack"),
      catalogTool("node", "Node"),
    ]);
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job({ id: "job-git", tool_id: "git", status: "running" }));
    store.applyJob(
      job({ id: "job-rail", tool_id: "railpack", status: "running", created_at: "2026-10-16T12:05:00Z" }),
    );
    getJobMock.mockResolvedValue({ job: job(), log: "", next_seq: 0 });

    store.openQueueModal();
    const wrapper = mountModal();
    await flushPromises();
    // Newest running job is followed by default.
    expect(selectedToolName(wrapper)).toBe("Railpack");

    await selectRow(wrapper, "Git");
    await flushPromises();
    expect(selectedToolName(wrapper)).toBe("Git");

    store.applyJob(
      job({ id: "job-node", tool_id: "node", status: "queued", created_at: "2026-10-16T12:10:00Z" }),
    );
    await flushPromises();
    expect(selectedToolName(wrapper)).toBe("Git");
    wrapper.unmount();
  });

  it("keeps a manual pick of a finished job when another job progresses", async () => {
    listToolsMock.mockResolvedValue([catalogTool("git", "Git"), catalogTool("railpack", "Railpack")]);
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    const finished = job({ id: "job-git", tool_id: "git", status: "failed" });
    store.applyJob(finished);
    store.applyJob(
      job({ id: "job-rail", tool_id: "railpack", status: "running", created_at: "2026-10-16T12:05:00Z" }),
    );
    getJobMock.mockResolvedValue({ job: finished, log: "why it failed\n", next_seq: 1 });

    store.openQueueModal();
    const wrapper = mountModal();
    await flushPromises();
    expect(selectedToolName(wrapper)).toBe("Railpack");

    // The user pins the failed job to read its log…
    await selectRow(wrapper, "Git");
    await flushPromises();
    expect(selectedToolName(wrapper)).toBe("Git");

    // …and a progress frame for the running job must not steal it back.
    store.applyJob(
      job({
        id: "job-rail",
        tool_id: "railpack",
        status: "running",
        created_at: "2026-10-16T12:05:00Z",
        started_at: "2026-10-16T12:05:01Z",
      }),
    );
    await flushPromises();
    expect(selectedToolName(wrapper)).toBe("Git");
    wrapper.unmount();
  });

  it("keeps an open-requested finished job across later frames", async () => {
    listToolsMock.mockResolvedValue([catalogTool("git", "Git"), catalogTool("railpack", "Railpack")]);
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    const finished = job({ id: "job-git", tool_id: "git", status: "failed" });
    store.applyJob(finished);
    store.applyJob(
      job({ id: "job-rail", tool_id: "railpack", status: "running", created_at: "2026-10-16T12:05:00Z" }),
    );
    getJobMock.mockResolvedValue({ job: finished, log: "why it failed\n", next_seq: 1 });

    store.openQueueModal("job-git");
    const wrapper = mountModal();
    await flushPromises();
    expect(selectedToolName(wrapper)).toBe("Git");

    store.applyJob(
      job({
        id: "job-rail",
        tool_id: "railpack",
        status: "running",
        created_at: "2026-10-16T12:05:00Z",
        started_at: "2026-10-16T12:05:01Z",
      }),
    );
    await flushPromises();
    expect(selectedToolName(wrapper)).toBe("Git");
    wrapper.unmount();
  });
});

describe("InstallJobLogPane", () => {
  /** paneLines builds N rendered lines ending at a base seq. */
  function paneLines(count: number, endSeq: number) {
    return Array.from({ length: count }, (_, i) => ({
      seq: endSeq - count + 1 + i,
      text: `l${endSeq - count + 1 + i}`,
    }));
  }

  /** mockScroll gives a jsdom element measurable scroll geometry. */
  function mockScroll(el: HTMLElement, scrollHeight: number, clientHeight: number, top: number) {
    Object.defineProperty(el, "scrollHeight", { value: scrollHeight, configurable: true });
    Object.defineProperty(el, "clientHeight", { value: clientHeight, configurable: true });
    el.scrollTop = top;
  }

  it("keeps following while the capped buffer rotates", async () => {
    const wrapper = mount(InstallJobLogPane, {
      global: { plugins: [i18n] },
      props: { title: "docker", state: "ready", lines: paneLines(500, 500) },
    });
    const el = wrapper.find(".log-pane").element as HTMLElement;
    mockScroll(el, 8000, 300, 7700);
    await wrapper.setProps({ lines: paneLines(500, 501) });
    await nextTick();

    // Length is frozen at the cap; only the last seq proves new content.
    expect(el.scrollTop).toBe(8000);
    wrapper.unmount();
  });

  it("pauses on scroll-up and jumps back on demand", async () => {
    const wrapper = mount(InstallJobLogPane, {
      global: { plugins: [i18n] },
      props: { title: "docker", state: "ready", lines: paneLines(10, 10) },
    });
    const el = wrapper.find(".log-pane").element as HTMLElement;
    mockScroll(el, 1000, 300, 700);
    await wrapper.setProps({ lines: paneLines(11, 11) });
    await nextTick();
    expect(el.scrollTop).toBe(1000);

    mockScroll(el, 1100, 300, 100);
    await wrapper.find(".log-pane").trigger("scroll");
    await wrapper.setProps({ lines: paneLines(12, 12) });
    await nextTick();
    expect(el.scrollTop).toBe(100);

    const jump = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Jump"));
    expect(jump).toBeDefined();
    await jump?.trigger("click");
    expect(el.scrollTop).toBe(1100);
    wrapper.unmount();
  });
});
