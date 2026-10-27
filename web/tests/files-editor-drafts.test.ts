/* global Storage:readonly, window:readonly */
import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { effectScope, nextTick } from "vue";
import type { EffectScope } from "vue";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    putContent: vi.fn(),
    getContent: vi.fn(),
  };
});

import { getContent, putContent } from "@/features/files/api/files";
import { useAuthStore } from "@/features/auth/stores/auth";
import EditorDraftDialog from "@/features/files/components/EditorDraftDialog.vue";
import { useEditorDrafts } from "@/features/files/composables/useEditorDrafts";
import editorEn from "@/features/files/locales/editorEn";
import {
  EDITOR_DRAFT_MAX_CHARS,
  EDITOR_DRAFT_MAX_DRAFTS,
  EDITOR_DRAFT_TTL_MS,
} from "@/features/files/schemas/editorDraft";
import { useEditorStore } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import { filesPane } from "@/features/files/stores/filesPane";
import {
  editorDraftStorageKey,
  editorDraftStoragePrefix,
  readEditorDraft,
  removeEditorDraft,
  writeEditorDraft,
} from "@/features/files/utils/editorDrafts";
import type { EditorDraftStorage } from "@/features/files/utils/editorDrafts";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";
import { clearSession as clearStoredSession } from "@/shared/api/token";

const getContentMock = vi.mocked(getContent);
const putContentMock = vi.mocked(putContent);

const server = "srv";
const user = "user-1";
const prefix = editorDraftStoragePrefix(server, user);

function memoryStorage(): EditorDraftStorage & { dump: Map<string, string> } {
  const dump = new Map<string, string>();
  return {
    dump,
    get length() {
      return dump.size;
    },
    key: (index: number) => [...dump.keys()][index] ?? null,
    getItem: (key: string) => dump.get(key) ?? null,
    setItem: (key: string, value: string) => void dump.set(key, value),
    removeItem: (key: string) => void dump.delete(key),
  };
}

function throwingStorage(): EditorDraftStorage {
  return {
    length: 0,
    key: () => {
      throw new Error("denied");
    },
    getItem: () => {
      throw new Error("denied");
    },
    setItem: () => {
      throw new Error("denied");
    },
    removeItem: () => {
      throw new Error("denied");
    },
  };
}

function draft(overrides: Partial<Parameters<typeof writeEditorDraft>[2]> = {}) {
  return {
    root: "volumes",
    path: "app.yaml",
    doc: "edited\n",
    etag: '"v1"',
    eol: "\n" as const,
    bom: false,
    savedAt: Date.now(),
    ...overrides,
  };
}

function content(body: string, etag = '"v1"') {
  return { content: body, etag, sha256: "abc", size: body.length, mode: 0o100644 };
}

function account(id: string) {
  return { id, email: `${id}@example.com`, created_at: "2026-10-01T00:00:00Z" };
}

function draftKeys(): string[] {
  const keys: string[] = [];
  for (let index = 0; index < window.localStorage.length; index += 1) {
    const key = window.localStorage.key(index);
    if (key?.startsWith("gotham.files.draft.v1.")) {
      keys.push(key);
    }
  }
  return keys;
}

let scope: EffectScope | null = null;

beforeEach(() => {
  window.localStorage.clear();
  setActivePinia(createPinia());
  const files = useFilesStore();
  files.roots = [{ name: "volumes", path: "/v", writable: true, free_bytes: 1 }];
  files.rootName = "volumes";
  const auth = useAuthStore();
  auth.user = {
    id: user,
    email: "user-1@example.com",
    created_at: "2026-10-01T00:00:00Z",
  };
  getContentMock.mockReset();
  putContentMock.mockReset();
});

afterEach(() => {
  scope?.stop();
  scope = null;
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("editor draft storage", () => {
  it("scopes keys by server and user and keeps root/path unambiguous", () => {
    expect(editorDraftStoragePrefix("", user)).toBe("");
    expect(editorDraftStoragePrefix(server, "")).toBe("");
    expect(editorDraftStoragePrefix("other", user)).not.toBe(prefix);
    const key = editorDraftStorageKey(prefix, "volumes", "etc/app.yaml");
    expect(key.startsWith(prefix)).toBe(true);
    expect(key).toContain(encodeURIComponent("etc/app.yaml"));
    expect(editorDraftStorageKey(prefix, "a.b", "c")).not.toBe(
      editorDraftStorageKey(prefix, "a", "b.c"),
    );
  });

  it("round-trips one draft", () => {
    const storage = memoryStorage();
    const key = editorDraftStorageKey(prefix, "volumes", "app.yaml");
    expect(writeEditorDraft(storage, prefix, draft({ doc: "next\n" }))).toBe(true);
    expect(readEditorDraft(storage, key)?.doc).toBe("next\n");
  });

  it("drops corrupt and oversized entries", () => {
    const storage = memoryStorage();
    const key = editorDraftStorageKey(prefix, "volumes", "app.yaml");
    storage.setItem(key, "{not json");
    expect(readEditorDraft(storage, key)).toBeNull();
    expect(storage.getItem(key)).toBeNull();

    const huge = "x".repeat(EDITOR_DRAFT_MAX_CHARS + 1);
    expect(writeEditorDraft(storage, prefix, draft({ doc: huge }))).toBe(false);
    expect(storage.getItem(key)).toBeNull();
  });

  it("expires drafts past the retention window", () => {
    const storage = memoryStorage();
    const key = editorDraftStorageKey(prefix, "volumes", "app.yaml");
    const now = Date.now();
    writeEditorDraft(storage, prefix, draft({ savedAt: now - EDITOR_DRAFT_TTL_MS - 1 }), now);
    expect(readEditorDraft(storage, key, now)).toBeNull();
    expect(storage.getItem(key)).toBeNull();
  });

  it("caps the number of drafts per server+user, oldest evicted first", () => {
    const storage = memoryStorage();
    const now = Date.now();
    for (let index = 0; index < EDITOR_DRAFT_MAX_DRAFTS + 2; index += 1) {
      writeEditorDraft(
        storage,
        prefix,
        draft({ path: `file-${index}.txt`, savedAt: now + index }),
        now + index,
      );
    }
    expect(storage.length).toBe(EDITOR_DRAFT_MAX_DRAFTS);
    expect(
      readEditorDraft(storage, editorDraftStorageKey(prefix, "volumes", "file-0.txt")),
    ).toBeNull();
    expect(
      readEditorDraft(
        storage,
        editorDraftStorageKey(prefix, "volumes", `file-${EDITOR_DRAFT_MAX_DRAFTS + 1}.txt`),
      ),
    ).not.toBeNull();
  });

  it("degrades silently when storage throws", () => {
    const storage = throwingStorage();
    expect(writeEditorDraft(storage, prefix, draft())).toBe(false);
    expect(readEditorDraft(storage, "gotham.files.draft.v1.x/y")).toBeNull();
    expect(() => removeEditorDraft(storage, "gotham.files.draft.v1.x/y")).not.toThrow();
  });
});

describe("editor drafts composable", () => {
  async function start(body = "base\n", etag = '"v1"') {
    getContentMock.mockResolvedValue(content(body, etag));
    const editor = useEditorStore();
    scope = effectScope();
    const drafts = scope.run(() => useEditorDrafts(() => server))!;
    await editor.openFile(server, "volumes", "app.yaml");
    await nextTick();
    return { editor, drafts };
  }

  it("persists a dirty buffer after the debounce and drops it after save", async () => {
    vi.useFakeTimers();
    const { editor, drafts } = await start();
    const tab = editor.activeTab!;
    editor.setDoc(tab.id, "local edit\n");
    await nextTick();

    const key = editorDraftStorageKey(prefix, "volumes", "app.yaml");
    vi.advanceTimersByTime(800);
    expect(readEditorDraft(window.localStorage, key)?.doc).toBe("local edit\n");

    putContentMock.mockResolvedValue({
      name: "app.yaml",
      kind: "file",
      size: 12,
      mode: 0o100644,
      modified_at: "2026-10-18T10:00:00Z",
      etag: '"v2"',
    });
    expect(await editor.save(tab.id)).toBe("saved");
    await nextTick();
    expect(readEditorDraft(window.localStorage, key)).toBeNull();
    expect(drafts.prompt.value).toBeNull();
  });

  it("keeps the newer draft when another pane flushes a stale buffer", async () => {
    vi.useFakeTimers();
    getContentMock.mockResolvedValue(content("base\n", '"v1"'));
    const right = filesPane("right").files;
    right.roots = [{ name: "volumes", path: "/v", writable: true, free_bytes: 1 }];
    right.rootName = "volumes";
    const leftEditor = useEditorStore("left");
    const rightEditor = useEditorStore("right");
    const leftScope = effectScope();
    const rightScope = effectScope();
    leftScope.run(() => useEditorDrafts(() => server, "left"));
    rightScope.run(() => useEditorDrafts(() => server, "right"));
    await leftEditor.openFile(server, "volumes", "app.yaml");
    await rightEditor.openFile(server, "volumes", "app.yaml");
    await nextTick();

    leftEditor.setDoc(leftEditor.activeTab!.id, "left edit\n");
    await nextTick();
    vi.advanceTimersByTime(800);
    const key = editorDraftStorageKey(prefix, "volumes", "app.yaml");
    expect(readEditorDraft(window.localStorage, key)?.doc).toBe("left edit\n");

    rightEditor.setDoc(rightEditor.activeTab!.id, "right edit\n");
    await nextTick();
    vi.advanceTimersByTime(800);
    expect(readEditorDraft(window.localStorage, key)?.doc).toBe("right edit\n");

    leftScope.stop();
    expect(readEditorDraft(window.localStorage, key)?.doc).toBe("right edit\n");
    rightScope.stop();
  });

  it("keeps the other pane's newer draft when one pane closes its tab", async () => {
    vi.useFakeTimers();
    getContentMock.mockResolvedValue(content("base\n", '"v1"'));
    const right = filesPane("right").files;
    right.roots = [{ name: "volumes", path: "/v", writable: true, free_bytes: 1 }];
    right.rootName = "volumes";
    const leftEditor = useEditorStore("left");
    const rightEditor = useEditorStore("right");
    const leftScope = effectScope();
    const rightScope = effectScope();
    leftScope.run(() => useEditorDrafts(() => server, "left"));
    rightScope.run(() => useEditorDrafts(() => server, "right"));
    await leftEditor.openFile(server, "volumes", "app.yaml");
    await rightEditor.openFile(server, "volumes", "app.yaml");
    await nextTick();

    leftEditor.setDoc(leftEditor.activeTab!.id, "left edit\n");
    await nextTick();
    vi.advanceTimersByTime(800);
    rightEditor.setDoc(rightEditor.activeTab!.id, "right edit\n");
    await nextTick();
    vi.advanceTimersByTime(800);
    const key = editorDraftStorageKey(prefix, "volumes", "app.yaml");
    expect(readEditorDraft(window.localStorage, key)?.doc).toBe("right edit\n");

    leftEditor.close(leftEditor.activeTab!.id);
    await nextTick();
    vi.advanceTimersByTime(800);

    expect(readEditorDraft(window.localStorage, key)?.doc).toBe("right edit\n");
    leftScope.stop();
    rightScope.stop();
  });

  it("keeps the other pane's newer draft when one pane saves", async () => {
    vi.useFakeTimers();
    getContentMock.mockResolvedValue(content("base\n", '"v1"'));
    putContentMock.mockResolvedValue({
      name: "app.yaml",
      kind: "file",
      size: 12,
      mode: 0o100644,
      modified_at: "2026-10-18T10:00:00Z",
      etag: '"v2"',
    });
    const right = filesPane("right").files;
    right.roots = [{ name: "volumes", path: "/v", writable: true, free_bytes: 1 }];
    right.rootName = "volumes";
    const leftEditor = useEditorStore("left");
    const rightEditor = useEditorStore("right");
    const leftScope = effectScope();
    const rightScope = effectScope();
    leftScope.run(() => useEditorDrafts(() => server, "left"));
    rightScope.run(() => useEditorDrafts(() => server, "right"));
    await leftEditor.openFile(server, "volumes", "app.yaml");
    await rightEditor.openFile(server, "volumes", "app.yaml");
    await nextTick();

    leftEditor.setDoc(leftEditor.activeTab!.id, "left edit\n");
    await nextTick();
    vi.advanceTimersByTime(800);
    rightEditor.setDoc(rightEditor.activeTab!.id, "right edit\n");
    await nextTick();
    vi.advanceTimersByTime(800);
    const key = editorDraftStorageKey(prefix, "volumes", "app.yaml");
    expect(readEditorDraft(window.localStorage, key)?.doc).toBe("right edit\n");

    expect(await leftEditor.save(leftEditor.activeTab!.id)).toBe("saved");
    await nextTick();
    vi.advanceTimersByTime(800);

    expect(readEditorDraft(window.localStorage, key)?.doc).toBe("right edit\n");
    leftScope.stop();
    rightScope.stop();
  });

  it("offers a stored draft on reopen and restores the exact buffer", async () => {
    const key = editorDraftStorageKey(prefix, "volumes", "app.yaml");
    writeEditorDraft(window.localStorage, prefix, draft({ doc: "recovered\n" }));
    const { editor, drafts } = await start();

    expect(drafts.prompt.value).not.toBeNull();
    expect(drafts.prompt.value?.name).toBe("app.yaml");
    expect(drafts.prompt.value?.conflict).toBe(false);

    drafts.restore();
    expect(editor.activeTab?.doc).toBe("recovered\n");
    expect(editor.isDirty(editor.activeTab!)).toBe(true);
    expect(drafts.prompt.value).toBeNull();
    expect(readEditorDraft(window.localStorage, key)?.doc).toBe("recovered\n");
  });

  it("warns when the base etag moved and discards on request", async () => {
    const key = editorDraftStorageKey(prefix, "volumes", "app.yaml");
    writeEditorDraft(
      window.localStorage,
      prefix,
      draft({ doc: "recovered\n", etag: '"v0"' }),
    );
    const { editor, drafts } = await start("base\n", '"v1"');

    expect(drafts.prompt.value?.conflict).toBe(true);
    drafts.discard();
    expect(drafts.prompt.value).toBeNull();
    expect(readEditorDraft(window.localStorage, key)).toBeNull();
    expect(editor.activeTab?.doc).toBe("base\n");
  });

  it("keeps the draft base etag after a conflicting restore, so Save hits the 409 flow", async () => {
    vi.useFakeTimers();
    const key = editorDraftStorageKey(prefix, "volumes", "app.yaml");
    writeEditorDraft(
      window.localStorage,
      prefix,
      draft({ doc: "recovered\n", etag: '"v0"' }),
    );
    const { editor, drafts } = await start("base\n", '"v1"');
    const tab = editor.activeTab!;

    drafts.restore();
    expect(tab.etag).toBe('"v0"');
    expect(tab.doc).toBe("recovered\n");

    await nextTick();
    vi.advanceTimersByTime(800);
    expect(readEditorDraft(window.localStorage, key)?.etag).toBe('"v0"');

    putContentMock.mockRejectedValueOnce({ message: "conflict", status: 409 });
    expect(await editor.save(tab.id)).toBe("conflict");
    expect(editor.conflictId).toBe(tab.id);
    expect(putContentMock).toHaveBeenCalledWith(
      server,
        expect.objectContaining({ etag: '"v0"', overwrite: false }),
    );
  });

  it("does not restore over a tab that reloaded while the prompt was open", async () => {
    writeEditorDraft(
      window.localStorage,
      prefix,
      draft({ doc: "recovered\n", etag: '"v1"' }),
    );
    const { editor, drafts } = await start("base\n", '"v1"');
    const tab = editor.activeTab!;
    expect(drafts.prompt.value).not.toBeNull();

    getContentMock.mockResolvedValue(content("server moved\n", '"v2"'));
    await editor.reload(tab.id);
    await nextTick();

    drafts.restore();
    expect(tab.doc).toBe("server moved\n");
    expect(drafts.prompt.value?.loadedEtag).toBe('"v2"');
    expect(drafts.prompt.value?.conflict).toBe(true);
  });

  it("drops the prompt when its tab was closed before the answer", async () => {
    writeEditorDraft(window.localStorage, prefix, draft({ doc: "recovered\n" }));
    const { editor, drafts } = await start();
    const tab = editor.activeTab!;

    editor.close(tab.id);
    await nextTick();
    expect(() => drafts.restore()).not.toThrow();
    expect(drafts.prompt.value).toBeNull();
    expect(editor.tabs).toHaveLength(0);
  });

  it("never persists a read-only root", async () => {
    vi.useFakeTimers();
    const files = useFilesStore();
    files.roots = [{ name: "compose", path: "/c", writable: false, free_bytes: 0 }];
    files.rootName = "compose";
    getContentMock.mockResolvedValue(content("base\n"));
    const editor = useEditorStore();
    scope = effectScope();
    scope.run(() => useEditorDrafts(() => server));
    await editor.openFile(server, "compose", "docker-compose.yaml");
    editor.setDoc(editor.activeTab!.id, "edited\n");
    await nextTick();

    expect(
      readEditorDraft(
        window.localStorage,
        editorDraftStorageKey(prefix, "compose", "docker-compose.yaml"),
      ),
    ).toBeNull();
  });

  it("keeps editing when the browser refuses a write", async () => {
    vi.useFakeTimers();
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });
    const { editor } = await start();
    expect(() => editor.setDoc(editor.activeTab!.id, "edited\n")).not.toThrow();
    await nextTick();
    vi.advanceTimersByTime(800);
    expect(editor.activeTab?.doc).toBe("edited\n");
  });
});

describe("editor draft purge on sign-out", () => {
  it("clears every draft on logout but keeps unrelated keys", () => {
    const other = editorDraftStoragePrefix(server, "user-2");
    writeEditorDraft(window.localStorage, prefix, draft({ path: "one.env" }));
    writeEditorDraft(window.localStorage, other, draft({ path: "two.env" }));
    window.localStorage.setItem("gotham.files.tabs.v1.srv.user-1", "{}");

    useAuthStore().clearSession();

    expect(draftKeys()).toEqual([]);
    expect(window.localStorage.getItem("gotham.files.tabs.v1.srv.user-1")).toBe("{}");
  });

  it("clears drafts on the forced session-expiry path", () => {
    writeEditorDraft(window.localStorage, prefix, draft());
    clearStoredSession();
    expect(draftKeys()).toEqual([]);
  });

  it("clears the previous account's drafts on a user switch", () => {
    const auth = useAuthStore();
    auth.setSession({
      user: account("user-1"),
      access_token: "a",
      refresh_token: "r",
    });
    writeEditorDraft(window.localStorage, prefix, draft());

    auth.setSession({
      user: account("user-1"),
      access_token: "b",
      refresh_token: "s",
    });
    expect(draftKeys()).toHaveLength(1);

    auth.setSession({
      user: account("user-2"),
      access_token: "c",
      refresh_token: "t",
    });
    expect(draftKeys()).toEqual([]);
  });
});

describe("editor draft dialog", () => {
  beforeEach(() => {
    resetLocaleState();
    syncComposerLocale("en");
    i18n.global.mergeLocaleMessage("en", { files: { editor: editorEn } });
  });

  it("offers Restore and Discard, keeping the conflict warning visible", async () => {
    const prompt = {
      tabId: "tab-1",
      name: "app.yaml",
      root: "volumes",
      path: "etc/app.yaml",
      doc: "edited\n",
      eol: "\n" as const,
      bom: false,
      savedAt: Date.UTC(2026, 9, 18, 10, 0, 0),
      baseEtag: '"v0"',
      loadedEtag: '"v1"',
      baseDoc: "base\n",
      conflict: true,
    };
    const wrapper = mount(EditorDraftDialog, {
      props: { prompt },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });

    expect(wrapper.text()).toContain(String(i18n.global.t("files.editor.draftConflict")));
    const restore = wrapper
      .findAll("button")
      .find((button) => button.text().trim() === String(i18n.global.t("files.editor.draftRestore")));
    const discard = wrapper
      .findAll("button")
      .find((button) => button.text().trim() === String(i18n.global.t("files.editor.draftDiscard")));
    expect(restore).toBeDefined();
    expect(discard).toBeDefined();
    await restore!.trigger("click");
    await discard!.trigger("click");
    expect(wrapper.emitted("restore")).toHaveLength(1);
    expect(wrapper.emitted("discard")).toHaveLength(1);
  });
});
