// Editor tab store (P18 step 8): dirty tracking, byte-faithful round trip,
// save races, the 409 conflict flow and the rename/move/delete tab sync. The
// API module is mocked so the store is exercised without axios or the DOM.
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
import type { FilesEntry } from "@/features/files/schemas/files";
import { useEditorStore } from "@/features/files/stores/editor";
import { serializeDoc, splitDoc } from "@/features/files/utils/editor";

const getContentMock = vi.mocked(getContent);
const putContentMock = vi.mocked(putContent);

const entry: FilesEntry = {
  name: "app.yaml",
  kind: "file",
  size: 24,
  mode: 0o100644,
  modified_at: "2026-10-12T09:02:00Z",
  etag: '"v2"',
};

/** content builds a FilesContent response. */
function content(body: string, etag = '"v1"') {
  return { content: body, etag, sha256: "abc", size: body.length, mode: 0o100644 };
}

/** deferred returns a promise with its resolve exposed. */
function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

beforeEach(() => {
  setActivePinia(createPinia());
  getContentMock.mockReset();
  putContentMock.mockReset();
});

describe("editor tab store", () => {
  it("opens a file, decodes its shape and starts clean", async () => {
    getContentMock.mockResolvedValueOnce(content("services:\n  web:\n"));
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "app.yaml");

    expect(store.tabs).toHaveLength(1);
    const tab = store.activeTab!;
    expect(tab.status).toBe("ready");
    expect(tab.doc).toBe("services:\n  web:\n");
    expect(tab.language).toBe("yaml");
    expect(store.isDirty(tab)).toBe(false);
    expect(store.hasDirty).toBe(false);
  });

  it("round-trips a CRLF + BOM file byte-identically", async () => {
    const original = "\uFEFFline1\r\nline2\r\nline3";
    getContentMock.mockResolvedValueOnce(content(original, '"v1"'));
    putContentMock.mockResolvedValueOnce({ ...entry, etag: '"v2"' });

    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "app.yaml");
    const tab = store.activeTab!;
    // The editor text is LF-normalised and BOM-free.
    expect(tab.doc).toBe("line1\nline2\nline3");
    expect(tab.eol).toBe("\r\n");
    expect(tab.bom).toBe(true);
    expect(serializeDoc({ doc: tab.doc, eol: tab.eol, bom: tab.bom })).toBe(original);

    const outcome = await store.save(tab.id, "team");
    expect(outcome).toBe("saved");
    expect(putContentMock).toHaveBeenCalledWith(
      "srv",
      "team",
      expect.objectContaining({ content: original, etag: '"v1"', overwrite: false }),
    );
  });

  it("splitDoc/serializeDoc are inverses for LF, CRLF and BOM", () => {
    for (const original of ["a\nb\n", "a\r\nb\r\n", "\uFEFFa\r\nb"]) {
      const shape = splitDoc(original);
      expect(serializeDoc(shape)).toBe(original);
    }
  });

  it("clears dirty on save only when the doc did not change in flight", async () => {
    getContentMock.mockResolvedValueOnce(content("start"));
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "app.yaml");
    const tab = store.activeTab!;

    // A plain edit + save clears dirty.
    store.setDoc(tab.id, "edited");
    expect(store.isDirty(tab)).toBe(true);
    putContentMock.mockResolvedValueOnce({ ...entry, etag: '"v2"' });
    await store.save(tab.id, "team");
    expect(store.isDirty(tab)).toBe(false);
    expect(tab.etag).toBe('"v2"');

    // A change during the save keeps the tab dirty; original advances to the
    // exact text that was sent.
    const pending = deferred<FilesEntry>();
    putContentMock.mockReturnValueOnce(pending.promise);
    store.setDoc(tab.id, "first");
    const saving = store.save(tab.id, "team");
    store.setDoc(tab.id, "second");
    pending.resolve({ ...entry, etag: '"v3"' });
    expect(await saving).toBe("saved");
    expect(tab.original).toBe("first");
    expect(tab.doc).toBe("second");
    expect(store.isDirty(tab)).toBe(true);
  });

  it("arms the conflict prompt on a 409 and clears it on reload", async () => {
    getContentMock.mockResolvedValueOnce(content("start", '"v1"'));
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "app.yaml");
    const tab = store.activeTab!;
    store.setDoc(tab.id, "mine");

    putContentMock.mockRejectedValueOnce({ message: "conflict", status: 409 });
    const outcome = await store.save(tab.id, "team");
    expect(outcome).toBe("conflict");
    expect(store.conflictId).toBe(tab.id);

    getContentMock.mockResolvedValueOnce(content("theirs", '"v9"'));
    await store.reload(tab.id, "team");
    expect(store.conflictId).toBeNull();
    expect(tab.doc).toBe("theirs");
    expect(tab.original).toBe("theirs");
    expect(store.isDirty(tab)).toBe(false);
  });

  it("overwrite force-saves with overwrite=true", async () => {
    getContentMock.mockResolvedValueOnce(content("start", '"v1"'));
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "app.yaml");
    const tab = store.activeTab!;
    store.setDoc(tab.id, "mine");
    putContentMock.mockResolvedValueOnce({ ...entry, etag: '"v10"' });

    const outcome = await store.overwrite(tab.id, "team");
    expect(outcome).toBe("saved");
    expect(putContentMock).toHaveBeenCalledWith(
      "srv",
      "team",
      expect.objectContaining({ overwrite: true }),
    );
    expect(store.isDirty(tab)).toBe(false);
  });

  it("save-as-copy writes a sibling and retargets the tab", async () => {
    getContentMock.mockResolvedValueOnce(content("start"));
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "config/app.yaml");
    const tab = store.activeTab!;
    store.setDoc(tab.id, "mine");
    putContentMock.mockResolvedValueOnce({ ...entry, name: "app.yaml.copy", etag: '"c1"' });

    const outcome = await store.saveAsCopy(tab.id, "team", "app.yaml.copy");
    expect(outcome).toBe("saved");
    expect(putContentMock).toHaveBeenCalledWith(
      "srv",
      "team",
      expect.objectContaining({ path: "config/app.yaml.copy", overwrite: true }),
    );
    expect(tab.path).toBe("config/app.yaml.copy");
    expect(tab.name).toBe("app.yaml.copy");
    expect(store.isDirty(tab)).toBe(false);
  });

  it("renders a download-only tab on a 413 or 415 read", async () => {
    getContentMock.mockRejectedValueOnce({ message: "too large", status: 413 });
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "server.log");
    expect(store.activeTab!.status).toBe("large");

    getContentMock.mockRejectedValueOnce({ message: "not editable", status: 415 });
    await store.openFile("srv", "team", "volumes", "logo.png");
    expect(store.activeTab!.status).toBe("binary");
    // A download-only tab is never dirty and cannot be saved.
    expect(store.hasDirty).toBe(false);
    expect(await store.save(store.activeTab!.id, "team")).toBe("blocked");
  });

  it("blocks a mutation while a target tab is dirty, and retargets a clean one", async () => {
    getContentMock.mockResolvedValue(content("start"));
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "app.yaml");
    const tab = store.activeTab!;

    expect(store.blockingNames("volumes", "", ["app.yaml", "other.txt"])).toEqual([]);
    store.setDoc(tab.id, "mine");
    expect(store.blockingNames("volumes", "", ["app.yaml"])).toEqual(["app.yaml"]);

    // A rename retargets the open tab (the file op is blocked before this runs
    // for a dirty tab, but the sync must still follow a clean one).
    store.applyRename("volumes", "", "app.yaml", "renamed.yaml");
    expect(tab.path).toBe("renamed.yaml");
    expect(tab.name).toBe("renamed.yaml");
  });

  it("rebases an open tab through a move and closes it on delete", async () => {
    getContentMock.mockResolvedValue(content("start"));
    const store = useEditorStore();
    await store.openFile("srv", "team", "volumes", "src/app.yaml");
    const tab = store.activeTab!;

    store.applyMove("volumes", "src", "dst", ["app.yaml"]);
    expect(tab.path).toBe("dst/app.yaml");

    // A delete of an ancestor folder closes the tab; an unrelated delete does not.
    store.applyDelete("volumes", "other", ["thing.txt"]);
    expect(store.tabs).toHaveLength(1);
    store.applyDelete("volumes", "dst", ["app.yaml"]);
    expect(store.tabs).toHaveLength(0);
    expect(store.activeId).toBe("");
  });
});
