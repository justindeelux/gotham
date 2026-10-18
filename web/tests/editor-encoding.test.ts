// Editor encodings (JUS-222): a legacy file reports its charset, saves back in
// the same one, and a character the target cannot hold blocks the save instead
// of being dropped. The API module is mocked so the store runs without axios.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    getContent: vi.fn(),
    putContent: vi.fn(),
  };
});

import { getContent, putContent } from "@/features/files/api/files";
import type { FilesContent, FilesEntry } from "@/features/files/schemas/files";
import { useEditorStore } from "@/features/files/stores/editor";
import {
  encodingLabel,
  normalizeEditorEncoding,
  serializeDoc,
  unrepresentableChars,
} from "@/features/files/utils/editor";

const getContentMock = vi.mocked(getContent);
const putContentMock = vi.mocked(putContent);

const entry: FilesEntry = {
  name: "app.conf",
  kind: "file",
  size: 12,
  mode: 0o100644,
  modified_at: "2026-10-12T09:02:00Z",
  etag: '"v2"',
};

/** content builds a FilesContent response. */
function content(
  body: string,
  encoding = "utf-8",
  etag = '"v1"',
): FilesContent {
  return {
    content: body,
    encoding,
    etag,
    sha256: "abc",
    size: body.length,
    mode: 0o100644,
  };
}

beforeEach(() => {
  setActivePinia(createPinia());
  getContentMock.mockReset();
  putContentMock.mockReset();
});

describe("editor encoding helpers", () => {
  it("labels every supported charset", () => {
    expect(encodingLabel("utf-8")).toBe("UTF-8");
    expect(encodingLabel("gbk")).toBe("GBK");
    expect(encodingLabel("gb2312")).toBe("GB2312");
    expect(encodingLabel("big5")).toBe("BIG5");
    expect(encodingLabel("ascii")).toBe("ASCII");
  });

  it("falls back to UTF-8 for an unknown server charset", () => {
    expect(normalizeEditorEncoding(undefined)).toBe("utf-8");
    expect(normalizeEditorEncoding("utf-16")).toBe("utf-8");
    expect(normalizeEditorEncoding("GBK")).toBe("gbk");
  });

  it("only applies a BOM to UTF-8", () => {
    const shape = { doc: "a\n", eol: "\n" as const, bom: true };
    expect(serializeDoc(shape)).toBe("\uFEFFa\n");
    expect(serializeDoc({ ...shape, encoding: "gbk" })).toBe("a\n");
  });

  it("finds characters the target cannot represent", () => {
    expect(unrepresentableChars("plain", "utf-8")).toEqual([]);
    expect(unrepresentableChars("中文", "gbk")).toEqual([]);
    expect(unrepresentableChars("中文😀", "gb2312")).toEqual(["😀"]);
    expect(unrepresentableChars("中文😀", "big5")).toEqual(["😀"]);
    expect(unrepresentableChars("café", "ascii")).toEqual(["é"]);
    expect(unrepresentableChars("café", "gbk")).toEqual([]);
  });
});

describe("editor store encoding", () => {
  it("records the detected encoding of an opened file", async () => {
    getContentMock.mockResolvedValueOnce(content("配置已保存", "gb2312"));
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "app.conf");

    expect(store.activeTab?.encoding).toBe("gb2312");
    expect(store.activeTab?.status).toBe("ready");
  });

  it("saves back in the detected encoding", async () => {
    getContentMock.mockResolvedValueOnce(content("配置已保存", "gbk"));
    putContentMock.mockResolvedValueOnce({ ...entry, etag: '"v3"' });
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "app.conf");

    const outcome = await store.save(store.activeTab!.id, "team");

    expect(outcome).toBe("saved");
    expect(putContentMock).toHaveBeenCalledWith(
      "srv",
      "team",
      expect.objectContaining({ encoding: "gbk", content: "配置已保存" }),
    );
  });

  it("blocks a save with an unrepresentable character and keeps the tab dirty", async () => {
    getContentMock.mockResolvedValueOnce(content("配置", "gb2312"));
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "app.conf");
    const tab = store.activeTab!;
    store.setDoc(tab.id, "配置😀");

    const outcome = await store.save(tab.id, "team");

    expect(outcome).toBe("unsupported");
    expect(putContentMock).not.toHaveBeenCalled();
    expect(store.encodingWarning).toEqual({
      tabId: tab.id,
      encoding: "gb2312",
      characters: ["😀"],
    });
    expect(store.isDirty(tab)).toBe(true);
  });

  it("retries the blocked save as UTF-8", async () => {
    getContentMock.mockResolvedValueOnce(content("配置", "gb2312"));
    putContentMock.mockResolvedValueOnce({ ...entry, etag: '"v4"' });
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "app.conf");
    const tab = store.activeTab!;
    store.setDoc(tab.id, "配置😀");
    await store.save(tab.id, "team");

    const outcome = await store.saveAsUtf8(tab.id, "team");

    expect(outcome).toBe("saved");
    expect(tab.encoding).toBe("utf-8");
    expect(store.encodingWarning).toBeNull();
    expect(putContentMock).toHaveBeenCalledWith(
      "srv",
      "team",
      expect.objectContaining({ encoding: "utf-8", content: "配置😀" }),
    );
  });

  it("changes the save encoding of a tab", async () => {
    getContentMock.mockResolvedValueOnce(content("plain", "utf-8"));
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "app.conf");
    const tab = store.activeTab!;

    store.setEncoding(tab.id, "big5");

    expect(tab.encoding).toBe("big5");
  });
});
