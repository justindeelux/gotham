/* global Blob:readonly, URL:readonly, AbortSignal:readonly */
import { NModal } from "naive-ui";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    issueDownloadTicket: vi.fn(),
    fetchDownloadBlob: vi.fn(),
  };
});

import {
  fetchDownloadBlob,
  imageMimeType,
  issueDownloadTicket,
} from "@/features/files/api/files";
import ImageViewerDialog from "@/features/files/components/ImageViewerDialog.vue";
import type { FilesEntry } from "@/features/files/schemas/files";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const ticketMock = vi.mocked(issueDownloadTicket);
const blobMock = vi.mocked(fetchDownloadBlob);
const createObjectURLMock = vi.fn<(_blob: Blob) => string>();
const revokeObjectURLMock = vi.fn<(_url: string) => void>();
let nextUrlId = 0;

function entry(name: string): FilesEntry {
  return {
    name,
    kind: "file",
    size: 25190,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
  };
}

function props(overrides: Record<string, unknown> = {}) {
  return {
    show: false,
    serverId: "server-1",
      root: "srv",
    directory: "",
    entry: entry("logo.png"),
    ...overrides,
  };
}

function mountDialog(dialogProps: Record<string, unknown> = {}) {
  return mount(ImageViewerDialog, {
    props: props(dialogProps),
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
}

async function afterLeave(wrapper: ReturnType<typeof mount>): Promise<void> {
  wrapper.findComponent(NModal).vm.$emit("after-leave");
  await flushPromises();
}

function buttonWith(
  wrapper: ReturnType<typeof mount>,
  text: string,
): ReturnType<typeof wrapper.findAll>[number] | undefined {
  return wrapper.findAll("button").find((button) => button.text().includes(text));
}

function deferred<T>() {
  let resolve!: (_value: T) => void;
  let reject!: (_reason: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  resetLocaleState();
  registerDiscoveredCatalogs();
  syncComposerLocale("en");
  vi.clearAllMocks();
  nextUrlId = 0;
  ticketMock.mockResolvedValue("ticket-1");
  blobMock.mockResolvedValue(new Blob(["pixels"], { type: "image/png" }));
  createObjectURLMock.mockImplementation(() => `blob:mock-${nextUrlId++}`);
  (URL as unknown as Record<string, unknown>).createObjectURL =
    createObjectURLMock;
  (URL as unknown as Record<string, unknown>).revokeObjectURL =
    revokeObjectURLMock;
});

describe("image viewer dialog", () => {
  it("loads the image through the files API and shows it", async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ show: true });
    await flushPromises();

    expect(ticketMock).toHaveBeenCalledWith(
      "server-1",
        "srv",
      "logo.png",
    );
    expect(blobMock).toHaveBeenCalledWith(
      "server-1",
      "ticket-1",
      "logo.png",
      expect.any(AbortSignal),
    );
    const image = wrapper.find(".viewer-stage img");
    expect(image.exists()).toBe(true);
    expect(image.attributes("src")).toBe("blob:mock-0");
    expect(image.attributes("alt")).toBe("logo.png");
    wrapper.unmount();
  });

  it("revokes the URL after the leave transition, not on close", async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ show: true });
    await flushPromises();
    expect(createObjectURLMock).toHaveBeenCalledTimes(1);

    await wrapper.setProps({ show: false });
    await flushPromises();
    expect(wrapper.find(".viewer-stage img").exists()).toBe(false);
    expect(revokeObjectURLMock).not.toHaveBeenCalled();

    await afterLeave(wrapper);
    expect(revokeObjectURLMock).toHaveBeenCalledWith("blob:mock-0");
    wrapper.unmount();
  });

  it("revokes the URL on unmount", async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ show: true });
    await flushPromises();

    wrapper.unmount();
    expect(revokeObjectURLMock).toHaveBeenCalledWith("blob:mock-0");
  });

  it("drops a stale load closed before the ticket resolves", async () => {
    const ticket = deferred<string>();
    ticketMock.mockReturnValue(ticket.promise);
    const wrapper = mountDialog();
    await wrapper.setProps({ show: true });
    await flushPromises();
    expect(ticketMock).toHaveBeenCalledTimes(1);

    await wrapper.setProps({ show: false });
    ticket.resolve("ticket-late");
    await flushPromises();
    expect(blobMock).not.toHaveBeenCalled();
    expect(createObjectURLMock).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("shows the viewer copy for an unclassified failure and retries", async () => {
    blobMock.mockRejectedValue(new TypeError("network down"));
    const wrapper = mountDialog();
    await wrapper.setProps({ show: true });
    await flushPromises();

    expect(wrapper.text()).toContain("Could not load the image.");
    expect(createObjectURLMock).not.toHaveBeenCalled();

    blobMock.mockResolvedValue(new Blob(["pixels"], { type: "image/png" }));
    await buttonWith(wrapper, "Retry")?.trigger("click");
    await flushPromises();
    expect(ticketMock).toHaveBeenCalledTimes(2);
    expect(wrapper.find(".viewer-stage img").exists()).toBe(true);
    wrapper.unmount();
  });

  it("maps a classified failure onto the files catalog", async () => {
    blobMock.mockRejectedValue({ status: 404, message: "gone" });
    const wrapper = mountDialog();
    await wrapper.setProps({ show: true });
    await flushPromises();

    expect(wrapper.text()).toContain("Not found.");
    wrapper.unmount();
  });

  it("reports an undecodable body as a load error", async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ show: true });
    await flushPromises();
    expect(wrapper.find(".viewer-stage img").exists()).toBe(true);

    await wrapper.find(".viewer-stage img").trigger("error");
    expect(wrapper.text()).toContain("Could not load the image.");
    expect(revokeObjectURLMock).toHaveBeenCalledWith("blob:mock-0");
    expect(wrapper.find(".viewer-stage img").exists()).toBe(false);
    wrapper.unmount();
  });

  it("reloads when the entry changes while open", async () => {
    const wrapper = mountDialog();
    await wrapper.setProps({ show: true });
    await flushPromises();
    expect(ticketMock).toHaveBeenCalledTimes(1);

    await wrapper.setProps({ entry: entry("shot.webp") });
    await flushPromises();
    expect(ticketMock).toHaveBeenCalledTimes(2);
    expect(ticketMock).toHaveBeenLastCalledWith(
      "server-1",
        "srv",
      "shot.webp",
    );
    wrapper.unmount();
  });
});

describe("imageMimeType", () => {
  it("maps the viewer formats onto image MIME types", () => {
    expect(imageMimeType("logo.png")).toBe("image/png");
    expect(imageMimeType("photo.JPG")).toBe("image/jpeg");
    expect(imageMimeType("icon.svg")).toBe("image/svg+xml");
    expect(imageMimeType("frame.avif")).toBe("image/avif");
  });

  it("falls back to octet-stream for an unknown extension", () => {
    expect(imageMimeType("data.xyz")).toBe("application/octet-stream");
    expect(imageMimeType("noext")).toBe("application/octet-stream");
  });
});
