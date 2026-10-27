/* global AbortSignal, DOMException */
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/transfer", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/transfer")>();
  return {
    ...actual,
    planTransfer: vi.fn(),
    streamTransfer: vi.fn(),
  };
});

import {
  planTransfer,
  streamTransfer,
  TransferIncompleteError,
  TransferStreamError,
} from "@/features/files/api/transfer";
import type {
  TransferPlan,
  TransferSummary,
} from "@/features/files/schemas/transfer";
import {
  useFilesTransferStore,
} from "@/features/files/stores/transfer";
import type { TransferSource } from "@/features/files/stores/transfer";

const source: TransferSource = {
  serverId: "server-a",
  pane: "left",
  root: "srv",
  path: "reports",
  names: ["report.txt", "data"],
  sizes: { "report.txt": 2048 },
  directories: ["data"],
};

const destination = {
  serverId: "server-b",
  root: "volumes",
  path: "backups",
};

const plan: TransferPlan = {
  mode: "bundle",
  bounds: {
    max_bundle_bytes: 1073741824,
    max_expanded_bytes: 4294967296,
    max_entries: 100000,
    max_sources: 1024,
    max_concurrency: 4,
    bundle_available: true,
  },
  sources: [
    { name: "report.txt", kind: "file", size: 2048, conflict: false },
    { name: "data", kind: "directory", size: 0, conflict: false },
  ],
  conflicts: [],
  unsupported: [],
  available_bytes: 1048576,
};

function summary(overrides: Partial<TransferSummary> = {}): TransferSummary {
  return {
    transfer_id: "t-1",
    mode: "bundle",
    status: "copied",
    copied: 1,
    conflicts: 0,
    skipped: 0,
    failed: 0,
    cancelled: 0,
    uncertain: 0,
    entries: [{ name: "report.txt", status: "copied" }],
    skipped_count: 0,
    ...overrides,
  };
}

beforeEach(() => {
  setActivePinia(createPinia());
  vi.clearAllMocks();
  vi.mocked(planTransfer).mockResolvedValue(plan);
  vi.mocked(streamTransfer).mockResolvedValue(summary());
});

describe("workspace transfer store", () => {
  it("keeps the captured source and destination after the panes move", async () => {
    const liveSource: TransferSource = {
      ...source,
      names: [...source.names],
      sizes: { ...source.sizes },
      directories: [...source.directories],
    };
    const store = useFilesTransferStore();
    store.openPaneCopy(liveSource, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));

    const captured = store.target;
    expect(captured?.destination).toEqual(destination);

    // A pane switch or navigation after the capture must not retarget the run.
    liveSource.path = "elsewhere";
    liveSource.names.push("new.txt");
    await store.start();
    expect(vi.mocked(streamTransfer)).toHaveBeenCalledWith(
      "server-a",
      {
        source_root: "srv",
        source_directory: "reports",
        sources: ["report.txt", "data"],
        destination: {
          server_id: "server-b",
          root: "volumes",
          path: "backups",
        },
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(store.target?.destination).toEqual(destination);
  });

  it("marks a stream that ends without a summary as uncertain", async () => {
    vi.mocked(streamTransfer).mockRejectedValue(new TransferIncompleteError());
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    await store.start();

    expect(store.state).toBe("finished");
    expect(store.interrupted).toBe(true);
    expect(store.summary?.status).toBe("uncertain");
    expect(store.summary?.entries.every((entry) => entry.status === "uncertain")).toBe(
      true,
    );
  });

  it("reports a user cancellation separately from a summary", async () => {
    vi.mocked(streamTransfer).mockImplementation(
      (_serverId, _request, options) =>
        new Promise((_resolve, reject) => {
          options?.signal?.addEventListener("abort", () => {
            reject(new DOMException("aborted", "AbortError"));
          });
        }),
    );
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    const running = store.start();
    store.cancel();
    await running;

    expect(store.cancelledByUser).toBe(true);
    expect(store.summary?.status).toBe("cancelled");
    expect(store.summary?.entries.map((entry) => entry.status)).toEqual([
      "uncertain",
      "uncertain",
    ]);
    expect(store.summary?.entries.map((entry) => entry.reason)).toEqual([
      "unconfirmed",
      "unconfirmed",
    ]);
  });

  it("keeps acknowledged results when the user cancels", async () => {
    vi.mocked(streamTransfer).mockImplementation(
      (_serverId, _request, options) =>
        new Promise((_resolve, reject) => {
          options?.onEvent?.({
            type: "result",
            name: "report.txt",
            status: "copied",
          });
          options?.signal?.addEventListener("abort", () => {
            reject(new DOMException("aborted", "AbortError"));
          });
        }),
    );
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    const running = store.start();
    store.cancel();
    await running;

    expect(store.summary?.status).toBe("cancelled");
    expect(store.summary?.entries).toEqual([
      { name: "report.txt", status: "copied", reason: undefined },
      { name: "data", status: "uncertain", reason: "unconfirmed" },
    ]);
  });

  it("keeps each acknowledged result once", async () => {
    vi.mocked(streamTransfer).mockImplementation(
      async (_serverId, _request, options) => {
        options?.onEvent?.({
          type: "result",
          name: "report.txt",
          status: "copied",
        });
        options?.onEvent?.({
          type: "result",
          name: "report.txt",
          status: "failed",
          reason: "duplicate",
        });
        throw new TransferIncompleteError();
      },
    );
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    await store.start();

    expect(store.acknowledged).toEqual([
      { name: "report.txt", status: "copied", reason: undefined },
    ]);
    expect(store.summary?.entries).toEqual([
      { name: "report.txt", status: "copied" },
      { name: "data", status: "uncertain", reason: "interrupted" },
    ]);
  });

  it("lets the terminal summary win over streamed results", async () => {
    vi.mocked(streamTransfer).mockImplementation(
      async (_serverId, _request, options) => {
        options?.onEvent?.({
          type: "result",
          name: "report.txt",
          status: "copied",
        });
        return summary({
          status: "partial",
          copied: 0,
          failed: 1,
          entries: [{ name: "report.txt", status: "failed", reason: "busy" }],
        });
      },
    );
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    await store.start();

    expect(store.summary?.entries).toEqual([
      { name: "report.txt", status: "failed", reason: "busy" },
    ]);
    expect(store.summary?.failed).toBe(1);
  });

  it("does not replace a received summary with a later stream error", async () => {
    vi.mocked(streamTransfer).mockImplementation(
      async (_serverId, _request, options) => {
        options?.onEvent?.({
          type: "summary",
          status: "copied",
          summary: summary(),
        });
        throw new TransferStreamError("trailing event");
      },
    );
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    await store.start();

    expect(store.summary?.status).toBe("copied");
    expect(store.summary?.entries).toEqual([
      { name: "report.txt", status: "copied" },
    ]);
    expect(store.interrupted).toBe(false);
  });

  it("reports a non-2xx response as a start failure", async () => {
    vi.mocked(streamTransfer).mockRejectedValue({
      status: 503,
      message: "files are disabled",
      code: "disabled",
      cause: null,
    });
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    await store.start();

    expect(store.state).toBe("finished");
    expect(store.runError).toMatchObject({ status: 503 });
    expect(store.summary).toBeNull();
    expect(store.interrupted).toBe(false);
  });

  it("treats a connection failure before the first event as uncertain", async () => {
    vi.mocked(streamTransfer).mockRejectedValue(
      new TypeError("Failed to fetch"),
    );
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    await store.start();

    expect(store.runError).toBeNull();
    expect(store.interrupted).toBe(true);
    expect(store.summary?.status).toBe("uncertain");
    expect(
      store.summary?.entries.every((entry) => entry.status === "uncertain"),
    ).toBe(true);
  });

  it("treats a malformed first line as uncertain, not a start failure", async () => {
    vi.mocked(streamTransfer).mockRejectedValue(
      new TransferStreamError("transfer event has an unknown shape"),
    );
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    await store.start();

    expect(store.runError).toBeNull();
    expect(store.interrupted).toBe(true);
    expect(store.summary?.status).toBe("uncertain");
  });

  it("keeps the timeout reason and acknowledged entries", async () => {
    vi.mocked(streamTransfer).mockResolvedValue(
      summary({
        status: "partial",
        reason: "timeout",
        copied: 1,
        conflicts: 1,
        entries: [
          { name: "report.txt", status: "copied" },
          { name: "data", status: "conflict", reason: "destination exists" },
        ],
      }),
    );
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    await store.start();

    expect(store.summary?.reason).toBe("timeout");
    expect(store.summary?.copied).toBe(1);
    expect(store.summary?.conflicts).toBe(1);
  });

  it("retries only the chosen unresolved names through a fresh preflight", async () => {
    vi.mocked(streamTransfer).mockResolvedValue(
      summary({
        status: "partial",
        failed: 1,
        uncertain: 1,
        entries: [
          { name: "report.txt", status: "failed", reason: "busy" },
          { name: "data", status: "uncertain", reason: "interrupted" },
        ],
      }),
    );
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    await store.start();

    await store.requestPlanFor(["report.txt"]);

    expect(vi.mocked(planTransfer)).toHaveBeenLastCalledWith(
      "server-a",
      expect.objectContaining({ sources: ["report.txt"] }),
      expect.any(AbortSignal),
    );
    expect(store.state).toBe("ready");
  });

  it("allows only one operation per workspace", async () => {
    vi.mocked(streamTransfer).mockImplementation(
      (_serverId, _request, options) =>
        new Promise((_resolve, reject) => {
          options?.signal?.addEventListener("abort", () => {
            reject(new DOMException("aborted", "AbortError"));
          });
        }),
    );
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    const running = store.start();

    store.openPaneCopy(source, destination);
    expect(store.isBusy).toBe(true);
    expect(store.source).toMatchObject({ serverId: "server-a" });

    store.cancel();
    await running;
  });

  it("asks before leaving while a copy is running", async () => {
    vi.mocked(streamTransfer).mockImplementation(
      (_serverId, _request, options) =>
        new Promise((_resolve, reject) => {
          options?.signal?.addEventListener("abort", () => {
            reject(new DOMException("aborted", "AbortError"));
          });
        }),
    );
    const store = useFilesTransferStore();
    store.openPaneCopy(source, destination);
    await vi.waitFor(() => expect(store.state).toBe("ready"));
    const running = store.start();

    const leaving = store.confirmLeave();
    expect(store.leaveOpen).toBe(true);
    store.resolveLeave(true);
    await expect(leaving).resolves.toBe(true);
    await running;
  });
});
