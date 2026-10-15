// Folder size "Calculate" (JUS-162): the API client's route/params and the
// composable's idle → loading → done / error / cancel state machine. The axios
// layer is mocked, so the real api module runs without a DOM or network.
import { beforeEach, describe, expect, it, vi, type Mock } from "vitest";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  teamHeaders: (teamId: string) => (teamId ? { "X-Team-Id": teamId } : {}),
}));

import { http } from "@/shared/api/http";
import { calculateFolderSize } from "@/features/files/api/files";
import {
  cancelFolderSize,
  folderSizeState,
  requestFolderSize,
  resetFolderSizes,
} from "@/features/files/composables/useFolderSize";
import { filesFolderSizeSchema } from "@/features/files/schemas/files";

const get = http.get as unknown as Mock;

/** canceledError mirrors axios's abort error so isCanceledError recognises it. */
function canceledError(): Error {
  const error = new Error("canceled");
  error.name = "CanceledError";
  return error;
}

beforeEach(() => {
  get.mockReset();
  resetFolderSizes();
});

describe("calculateFolderSize", () => {
  it("GETs the size route with the root, path and team header", async () => {
    get.mockResolvedValueOnce({
      data: { bytes: 2048, entries: 3, skipped: 1, truncated: false },
    });

    const size = await calculateFolderSize("srv", "team-1", "volumes", "apps");

    expect(size).toEqual({ bytes: 2048, entries: 3, skipped: 1, truncated: false });
    const [url, config] = get.mock.calls[0];
    expect(url).toBe("/servers/srv/files/size");
    expect(config.params).toEqual({ root: "volumes", path: "apps" });
    expect(config.headers).toEqual({ "X-Team-Id": "team-1" });
  });

  it("validates the response shape", () => {
    expect(
      filesFolderSizeSchema.safeParse({
        bytes: 10,
        entries: 1,
        skipped: 0,
        truncated: true,
      }).success,
    ).toBe(true);
    expect(
      filesFolderSizeSchema.safeParse({ bytes: "10", truncated: "yes" }).success,
    ).toBe(false);
  });
});

describe("useFolderSize", () => {
  it("moves idle → loading → done with the result", async () => {
    let resolve!: (_value: unknown) => void;
    get.mockImplementationOnce(() => new Promise((r) => (resolve = r)));

    const promise = requestFolderSize("srv", "team", "volumes", "apps");
    expect(folderSizeState("srv", "volumes", "apps").status).toBe("loading");

    resolve({ data: { bytes: 4096, entries: 5, skipped: 0, truncated: true } });
    await promise;

    const state = folderSizeState("srv", "volumes", "apps");
    expect(state.status).toBe("done");
    expect(state.bytes).toBe(4096);
    expect(state.truncated).toBe(true);
  });

  it("records a localized error on failure", async () => {
    get.mockRejectedValueOnce({ status: 404, message: "not found" });

    await requestFolderSize("srv", "team", "volumes", "gone");

    const state = folderSizeState("srv", "volumes", "gone");
    expect(state.status).toBe("error");
    expect(state.error).toBeTruthy();
  });

  it("returns to idle when the caller cancels", async () => {
    get.mockImplementationOnce(
      (_url: string, config: { signal: AbortSignal }) =>
        new Promise((_resolve, reject) => {
          config.signal.addEventListener("abort", () => reject(canceledError()));
        }),
    );

    const promise = requestFolderSize("srv", "team", "volumes", "apps");
    expect(folderSizeState("srv", "volumes", "apps").status).toBe("loading");

    cancelFolderSize("srv", "volumes", "apps");
    await promise;

    expect(folderSizeState("srv", "volumes", "apps").status).toBe("idle");
  });

  it("ignores a second request while one is in flight", async () => {
    get.mockImplementationOnce(() => new Promise(() => {}));

    void requestFolderSize("srv", "team", "volumes", "apps");
    await requestFolderSize("srv", "team", "volumes", "apps");

    expect(get).toHaveBeenCalledTimes(1);
  });
});
