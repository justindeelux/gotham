// Toolchain App Store store (JUS-177): queue state, enqueue/cancel actions,
// realtime job-frame application, and the JUS-178 modal hook point. The API
// module is mocked so the store is exercised without axios or a socket.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

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
  listJobs,
  listTools,
  parseToolchainJobFrame,
  parseToolchainLogFrame,
} from "@/features/toolchain/api/toolchain";
import type { ToolchainJob, ToolchainTool } from "@/features/toolchain/api/toolchain";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";

const listToolsMock = vi.mocked(listTools);
const listJobsMock = vi.mocked(listJobs);
const enqueueJobsMock = vi.mocked(enqueueJobs);
const cancelJobMock = vi.mocked(cancelJob);

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

beforeEach(() => {
  setActivePinia(createPinia());
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
    expect(listToolsMock).toHaveBeenCalledWith("srv-1");
  });

  it("clears node state when switching nodes", async () => {
    const store = useToolchainStore();
    listJobsMock.mockImplementation((id: string) =>
      Promise.resolve(id === "srv-1" ? [job()] : []),
    );
    await store.subscribe("srv-1");
    expect(store.jobsList).toHaveLength(1);
    await store.subscribe("srv-2");
    expect(store.tools).toHaveLength(1);
    expect(store.jobsList).toHaveLength(0);
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
});

describe("queue modal hook", () => {
  it("opens and closes without rendering a modal (JUS-178 owns the markup)", async () => {
    const store = useToolchainStore();
    expect(store.queueModalOpen).toBe(false);
    store.openQueueModal();
    expect(store.queueModalOpen).toBe(true);
    store.closeQueueModal();
    expect(store.queueModalOpen).toBe(false);
  });
});

describe("parseToolchainJobFrame", () => {
  it("decodes a job transition frame and ignores other frames", () => {
    const parsed = parseToolchainJobFrame(frame("toolchain_job", JSON.stringify(job())));
    expect(parsed?.id).toBe("job-1");
    expect(parseToolchainJobFrame(frame("toolchain_log", "{}"))).toBeNull();
    expect(parseToolchainJobFrame("not json")).toBeNull();
  });
});

describe("parseToolchainLogFrame", () => {
  it("decodes one log line with its sequence", () => {
    const parsed = parseToolchainLogFrame(
      frame("toolchain_log", JSON.stringify({ job_id: "job-1", seq: 7, line: "pulling…\n" })),
    );
    expect(parsed).toMatchObject({ job_id: "job-1", seq: 7 });
    expect(parseToolchainLogFrame(frame("toolchain_job", "{}"))).toBeNull();
  });
});
