// Upload queue (P18 step 7): per-file progress, cancel, retry, and the
// conflict path. naive-ui's useMessage is stubbed (everything else stays real
// so the shared i18n module still resolves), and the API module is mocked.
import { beforeEach, describe, expect, it, vi } from "vitest";

const messages = {
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
  info: vi.fn(),
};

vi.mock("naive-ui", async (importOriginal) => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return { ...actual, useMessage: () => messages };
});

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return { ...actual, uploadFiles: vi.fn() };
});

import { uploadFiles } from "@/features/files/api/files";
import type { FilesUploadResult } from "@/features/files/schemas/files";
import { useFileUpload } from "@/features/files/composables/useFileUpload";

const target = {
  serverId: "server-1",
  teamId: "team-1",
  root: "volumes",
  path: "",
};

/** file builds a small File for the queue. */
function file(name: string, size = 8) {
  return new globalThis.File([new Uint8Array(size)], name);
}

/** result builds one success entry in the upload result array. */
function ok(name: string): FilesUploadResult {
  return {
    name,
    path: name,
    entry: {
      name,
      kind: "file",
      size: 8,
      mode: 0o100644,
      modified_at: "2026-10-12T09:02:00Z",
      etag: '"1"',
    },
  };
}

beforeEach(() => {
  vi.mocked(uploadFiles).mockReset();
  messages.error.mockReset();
  messages.success.mockReset();
  messages.warning.mockReset();
});

/** deferred returns a promise with its resolve exposed (batch-gate tests). */
function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

describe("useFileUpload", () => {
  it("uploads a file, reports progress and lands in done", async () => {
    vi.mocked(uploadFiles).mockImplementation(async (_s, _t, _files, options) => {
      options.onProgress?.({ loaded: 3, total: 8 });
      options.onProgress?.({ loaded: 8, total: 8 });
      return [ok("app.yaml")];
    });

    const upload = useFileUpload();
    upload.addFiles([file("app.yaml")]);
    expect(upload.pending.value).toBe(1);

    await upload.start(target);

    expect(upload.items.value[0]?.status).toBe("done");
    expect(upload.items.value[0]?.loaded).toBe(8);
    expect(upload.summary.value.done).toBe(1);
    // The upload override is passed through, so the 15 s default is lifted.
    expect(vi.mocked(uploadFiles)).toHaveBeenCalledWith(
      "server-1",
      "team-1",
      [expect.any(globalThis.File)],
      expect.objectContaining({ root: "volumes", path: "", overwrite: false }),
    );
  });

  it("marks a 409 as a conflict without a transport toast", async () => {
    vi.mocked(uploadFiles).mockResolvedValue([
      { name: "server.log", path: "server.log", error: "conflict" },
    ]);

    const upload = useFileUpload();
    upload.addFiles([file("server.log")]);
    await upload.start(target);

    const item = upload.items.value[0];
    expect(item?.status).toBe("conflict");
    expect(item?.errorToken).toBe("conflict");
    expect(item?.message).toBeTruthy();
    expect(upload.hasFailures.value).toBe(1);
    expect(messages.error).not.toHaveBeenCalled();
  });

  it("cancels the in-flight request and marks the row cancelled", async () => {
    vi.mocked(uploadFiles).mockImplementation(
      (_s, _t, _files, options) =>
        new Promise<FilesUploadResult[]>((_resolve, reject) => {
          options.signal?.addEventListener("abort", () =>
            reject(new Error("canceled")),
          );
        }),
    );

    const upload = useFileUpload();
    upload.addFiles([file("big.bin")]);
    const started = upload.start(target);
    upload.cancel(upload.items.value[0]!.id);
    await started;

    expect(upload.items.value[0]?.status).toBe("cancelled");
    expect(messages.error).not.toHaveBeenCalled();
  });

  it("retries a failed file and can replace a conflicting one", async () => {
    vi.mocked(uploadFiles)
      .mockRejectedValueOnce(new Error("network"))
      .mockResolvedValueOnce([ok("a.txt")]);

    const upload = useFileUpload();
    upload.addFiles([file("a.txt")]);
    await upload.start(target);

    const item = upload.items.value[0];
    expect(item?.status).toBe("error");
    expect(messages.error).toHaveBeenCalled();

    await upload.retry(item!.id, target, false);
    expect(item?.status).toBe("done");
  });

  it("retryFailed replaces a conflicting file", async () => {
    vi.mocked(uploadFiles)
      .mockResolvedValueOnce([{ name: "x", path: "x", error: "conflict" }])
      .mockResolvedValueOnce([ok("x")]);

    const upload = useFileUpload();
    upload.addFiles([file("x")]);
    await upload.start(target);
    expect(upload.items.value[0]?.status).toBe("conflict");

    await upload.retryFailed(target);
    expect(upload.items.value[0]?.status).toBe("done");
    // The retry of a skipped file replaces it (overwrite=true).
    expect(vi.mocked(uploadFiles)).toHaveBeenLastCalledWith(
      "server-1",
      "team-1",
      [expect.any(globalThis.File)],
      expect.objectContaining({ overwrite: true }),
    );
  });

  it("rejects a locally invalid name without calling the API", async () => {
    const upload = useFileUpload();
    upload.addFiles([file(".")]);
    await upload.start(target);

    expect(upload.items.value[0]?.status).toBe("error");
    expect(upload.items.value[0]?.errorToken).toBe("invalid");
    expect(vi.mocked(uploadFiles)).not.toHaveBeenCalled();
  });

  it("does not send a queued file removed during the batch", async () => {
    const gate = deferred<FilesUploadResult[]>();
    vi.mocked(uploadFiles)
      .mockImplementationOnce(() => gate.promise)
      .mockResolvedValueOnce([ok("b.txt")]);

    const upload = useFileUpload();
    upload.addFiles([file("a.bin"), file("b.txt")]);
    const started = upload.start(target);
    // a.bin is in flight; cancel the queued b.txt.
    upload.removeItem(upload.items.value[1]!.id);
    gate.resolve([ok("a.bin")]);
    await started;

    expect(vi.mocked(uploadFiles)).toHaveBeenCalledTimes(1);
    expect(upload.items.value.map((item) => item.file.name)).toEqual(["a.bin"]);
  });

  it("clearing the queue stops the files that were still queued", async () => {
    const gate = deferred<FilesUploadResult[]>();
    vi.mocked(uploadFiles)
      .mockImplementationOnce(() => gate.promise)
      .mockResolvedValue([ok("x")]);

    const upload = useFileUpload();
    upload.addFiles([file("a"), file("b"), file("c")]);
    const started = upload.start(target);
    upload.clear();
    gate.resolve([ok("a")]);
    await started;

    expect(vi.mocked(uploadFiles)).toHaveBeenCalledTimes(1);
    expect(upload.items.value).toEqual([]);
  });

  it("retry never starts a second runner while a batch is in flight", async () => {
    let inflight = 0;
    let peak = 0;
    const gate = deferred<FilesUploadResult[]>();
    vi.mocked(uploadFiles)
      .mockResolvedValueOnce([{ name: "a", path: "a", error: "error" }])
      .mockImplementation(async () => {
        inflight += 1;
        peak = Math.max(peak, inflight);
        const result = await gate.promise;
        inflight -= 1;
        return result;
      });

    const upload = useFileUpload();
    upload.addFiles([file("a"), file("b")]);
    const started = upload.start(target);

    await vi.waitFor(() => {
      if (upload.items.value[0]!.status !== "error") {
        throw new Error("a still in flight");
      }
      if (upload.items.value[1]!.status !== "uploading") {
        throw new Error("b has not started");
      }
    });

    const retried = upload.retry(upload.items.value[0]!.id, target, false);
    gate.resolve([ok("b")]);
    await Promise.all([started, retried]);

    expect(peak).toBe(1);
    expect(upload.items.value[0]!.status).toBe("done");
  });

  it("sends the trimmed name it validated", async () => {
    vi.mocked(uploadFiles).mockResolvedValue([ok("a.txt")]);

    const upload = useFileUpload();
    upload.addFiles([file("  a.txt  ")]);
    await upload.start(target);

    const sent = vi.mocked(uploadFiles).mock.calls[0]?.[2]?.[0];
    expect(sent?.name).toBe("a.txt");
  });
});
