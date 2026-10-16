// Toolchain App Store store (JUS-177, fix round 1): queue state,
// enqueue/cancel actions, socket lifecycle (disconnect on unmount/sign-out),
// reconnect resync, refresh race guards, and the JUS-178 modal hook point.
// The API module and the realtime client are mocked; socket callbacks are
// driven manually through the captured options.
import { createPinia, setActivePinia } from "pinia";
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
    enqueueJobs: vi.fn(),
    cancelJob: vi.fn(),
  };
});

import {
  cancelJob,
  enqueueJobs,
  isToolSelectable,
  listJobs,
  listTools,
  parseToolchainJobFrame,
  parseToolchainLogFrame,
} from "@/features/toolchain/api/toolchain";
import type { ToolchainJob, ToolchainTool } from "@/features/toolchain/api/toolchain";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";

const useWebSocketMock = vi.mocked(useWebSocket);
const listToolsMock = vi.mocked(listTools);
const listJobsMock = vi.mocked(listJobs);
const enqueueJobsMock = vi.mocked(enqueueJobs);
const cancelJobMock = vi.mocked(cancelJob);

const socketStub = {
  subscribe: vi.fn(),
  unsubscribe: vi.fn(),
  close: vi.fn(),
};

let capturedOptions: UseWebSocketOptions | null = null;

function tool(overrides: Partial<ToolchainTool> = {}): ToolchainTool {
  return {
    id: "docker",
    name: "Docker",
    installed: true,
    installable: true,
    uninstallable: false,
    needs_root: true,
    version_ok: true,
    ...overrides,
  };
}

function job(overrides: Partial<ToolchainJob> = {}): ToolchainJob {
  return {
    id: "job-1",
    server_id: "srv-1",
    tool_id: "docker",
    action: "install",
    status: "queued",
    created_at: "2026-10-16T12:00:00Z",
    ...overrides,
  };
}

/** frame wraps a payload as a realtime hub frame. */
function frame(type: string, data: string): string {
  return JSON.stringify({ channel: "logs:srv-1:toolchain", type, data });
}

/** socketMessage builds the client message the store's handler reads. */
function socketMessage(raw: string, kind: WebSocketMessage["kind"] = "data"): WebSocketMessage {
  return {
    raw,
    payload: null,
    channel: "logs:srv-1:toolchain",
    kind,
    receivedAt: Date.now(),
  };
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
  listToolsMock.mockReset();
  listJobsMock.mockReset();
  enqueueJobsMock.mockReset();
  cancelJobMock.mockReset();
  listToolsMock.mockResolvedValue([tool()]);
  listJobsMock.mockResolvedValue([]);
});

describe("subscribe", () => {
  it("loads the catalog and queue for the selected node", async () => {
    const store = useToolchainStore();
    listJobsMock.mockResolvedValue([job()]);
    await store.subscribe("srv-1");
    expect(store.serverId).toBe("srv-1");
    expect(store.tools).toHaveLength(1);
    expect(store.jobsList).toHaveLength(1);
    expect(socketStub.subscribe).toHaveBeenCalledWith("logs:srv-1:toolchain");
  });

  it("switches the channel subscription when switching nodes", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    await store.subscribe("srv-2");
    expect(socketStub.unsubscribe).toHaveBeenCalledWith("logs:srv-1:toolchain");
    expect(socketStub.subscribe).toHaveBeenCalledWith("logs:srv-2:toolchain");
  });
});

describe("disconnect and reset", () => {
  it("reset unsubscribes and closes the socket (sign-out leaves nothing live)", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.reset();
    expect(socketStub.unsubscribe).toHaveBeenCalledWith("logs:srv-1:toolchain");
    expect(socketStub.close).toHaveBeenCalled();
    expect(store.serverId).toBe("");
    // A later subscribe dials a fresh socket instead of reusing the dead one.
    await store.subscribe("srv-1");
    expect(useWebSocketMock).toHaveBeenCalledTimes(2);
  });

  it("disconnect is safe to call twice and with no socket", () => {
    const store = useToolchainStore();
    expect(() => store.disconnect()).not.toThrow();
    store.disconnect();
    expect(socketStub.close).not.toHaveBeenCalled();
  });

  it("applies live job frames from the channel into the queue", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    expect(capturedOptions).not.toBeNull();
    capturedOptions?.onMessage?.(
      socketMessage(frame("toolchain_job", JSON.stringify(job({ status: "running" })))),
    );
    expect(store.jobs["job-1"]?.status).toBe("running");
  });
});

describe("reconnect resync", () => {
  it("refreshes the queue on reconnect, not on the first connect", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    expect(capturedOptions).not.toBeNull();
    const callsAfterSubscribe = listToolsMock.mock.calls.length;
    capturedOptions?.onStatusChange?.("open");
    expect(listToolsMock.mock.calls.length).toBe(callsAfterSubscribe);
    capturedOptions?.onStatusChange?.("open");
    expect(listToolsMock.mock.calls.length).toBe(callsAfterSubscribe + 1);
  });

  it("refreshes when the subscription is denied", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    const callsAfterSubscribe = listToolsMock.mock.calls.length;
    capturedOptions?.onMessage?.(socketMessage("{}", "denied"));
    expect(listToolsMock.mock.calls.length).toBe(callsAfterSubscribe + 1);
  });
});

describe("refresh race", () => {
  it("keeps the newer node's data when an older fetch resolves late", async () => {
    const store = useToolchainStore();
    const first = deferred<ToolchainTool[]>();
    const second = deferred<ToolchainTool[]>();
    listToolsMock.mockImplementation((id: string) =>
      id === "srv-1" ? first.promise : second.promise,
    );
    listJobsMock.mockResolvedValue([]);
    const p1 = store.subscribe("srv-1");
    const p2 = store.subscribe("srv-2");
    first.resolve([tool({ id: "old-tool", name: "Old" })]);
    await p1;
    second.resolve([tool({ id: "new-tool", name: "New" })]);
    await p2;
    expect(store.serverId).toBe("srv-2");
    expect(store.tools.map((item) => item.id)).toEqual(["new-tool"]);
    expect(store.loading).toBe(false);
  });

  it("does not leak an older node's error into the newer node's view", async () => {
    const store = useToolchainStore();
    const first = deferred<ToolchainTool[]>();
    listToolsMock.mockImplementation((id: string) =>
      id === "srv-1" ? first.promise : Promise.resolve([tool()]),
    );
    listJobsMock.mockResolvedValue([]);
    const p1 = store.subscribe("srv-1");
    const p2 = store.subscribe("srv-2");
    await p2;
    first.reject(new Error("node A exploded"));
    await p1;
    expect(store.serverId).toBe("srv-2");
    expect(store.failure).toBeNull();
    expect(store.tools).toHaveLength(1);
    expect(store.loading).toBe(false);
  });
});

describe("enqueue", () => {
  it("merges created jobs and reports skipped ids", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    enqueueJobsMock.mockResolvedValue({
      jobs: [job({ id: "job-2", tool_id: "git" })],
      skipped: ["docker"],
    });
    const skipped = await store.enqueue(["git", "docker"], "install");
    expect(skipped).toEqual(["docker"]);
    expect(store.jobs["job-2"]?.tool_id).toBe("git");
  });
});

describe("cancel", () => {
  it("applies the canceled job", async () => {
    const store = useToolchainStore();
    await store.subscribe("srv-1");
    store.applyJob(job());
    cancelJobMock.mockResolvedValue(job({ status: "canceled" }));
    await store.cancel("job-1");
    expect(store.jobs["job-1"]?.status).toBe("canceled");
  });
});

describe("jobForTool", () => {
  it("reports the latest job per tool", () => {
    const store = useToolchainStore();
    expect(store.jobForTool("docker")).toBeNull();
    store.applyJob(job({ created_at: "2026-10-16T12:00:00Z", status: "succeeded" }));
    store.applyJob(job({ id: "job-2", created_at: "2026-10-16T13:00:00Z", status: "running" }));
    expect(store.jobForTool("docker")?.id).toBe("job-2");
    expect(store.activeJobs).toHaveLength(1);
  });

  it("never downgrades a terminal state from a delayed frame", () => {
    const store = useToolchainStore();
    store.applyJob(job({ status: "succeeded" }));
    store.applyJob(job({ status: "running" }));
    expect(store.jobs["job-1"]?.status).toBe("succeeded");
  });
});

describe("isToolSelectable", () => {
  it("allows only missing, installable tools", () => {
    expect(isToolSelectable(tool({ installed: false, installable: true }))).toBe(true);
    expect(isToolSelectable(tool({ installed: true, installable: true }))).toBe(false);
    expect(isToolSelectable(tool({ installed: false, installable: false }))).toBe(false);
    expect(
      isToolSelectable(tool({ installed: false, installable: false, uninstallable: false })),
    ).toBe(false);
  });
});

describe("queue modal hook", () => {
  it("opens with an optional job focus and closes clean (JUS-178 owns the markup)", () => {
    const store = useToolchainStore();
    expect(store.queueModalOpen).toBe(false);
    store.openQueueModal("job-1");
    expect(store.queueModalOpen).toBe(true);
    expect(store.queueModalJobId).toBe("job-1");
    store.closeQueueModal();
    expect(store.queueModalOpen).toBe(false);
    expect(store.queueModalJobId).toBeNull();
  });
});

describe("parseToolchainJobFrame", () => {
  it("decodes a job transition frame and ignores other frames", () => {
    const parsed = parseToolchainJobFrame(frame("toolchain_job", JSON.stringify(job())));
    expect(parsed?.id).toBe("job-1");
    expect(parseToolchainJobFrame(frame("toolchain_log", "{}"))).toBeNull();
    expect(parseToolchainJobFrame("not json")).toBeNull();
    expect(parseToolchainJobFrame(frame("toolchain_job", "{broken"))).toBeNull();
    expect(
      parseToolchainJobFrame(frame("toolchain_job", JSON.stringify({ id: "x" }))),
    ).toBeNull();
  });
});

describe("parseToolchainLogFrame", () => {
  it("decodes one log line with its sequence", () => {
    const parsed = parseToolchainLogFrame(
      frame("toolchain_log", JSON.stringify({ job_id: "job-1", seq: 7, line: "pulling…\n" })),
    );
    expect(parsed).toMatchObject({ job_id: "job-1", seq: 7 });
    expect(parseToolchainLogFrame(frame("toolchain_job", "{}"))).toBeNull();
    expect(
      parseToolchainLogFrame(frame("toolchain_log", JSON.stringify({ seq: 1 }))),
    ).toBeNull();
  });
});
