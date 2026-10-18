// Unsaved editor drafts (JUS-229): debounced persistence keyed by
// server+user+root+path, retention caps and TTL cleanup, silent degradation when
// storage is blocked, the Restore/Discard prompt on reopen (with a stale-etag
// warning), and draft removal after save or explicit discard. The files API is
// mocked so the store runs without axios or a real network.
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
  EDITOR_DRAFT_MAX_BYTES,
  EDITOR_DRAFT_MAX_DRAFTS,
  EDITOR_DRAFT_TTL_MS,
} from "@/features/files/schemas/editorDraft";
import { useEditorStore } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import {
  editorDraftStorageKey,
  editorDraftStoragePrefix,
  readEditorDraft,
  removeEditorDraft,
  writeEditorDraft,
} from "@/features/files/utils/editorDrafts";
import type { EditorDraftStorage } from "@/features/files/utils/editorDrafts";
import { useTeamsStore } from "@/features/teams";
import type { Team } from "@/features/teams/api/teams";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const getContentMock = vi.mocked(getContent);
const putContentMock = vi.mocked(putContent);

const server = "srv";
const user = "user-1";
const prefix = editorDraftStoragePrefix(server, user);

/** memoryStorage builds an in-memory Storage double. */
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

/** throwingStorage fails every access (private mode / blocked storage). */
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

/** draft builds one valid draft payload. */
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

/** content builds a FilesContent response. */
function content(body: string, etag = '"v1"') {
  return { content: body, etag, sha256: "abc", size: body.length, mode: 0o100644 };
}

/** team builds one team with a role. */
function team(role: Team["role"]): Team {
  return {
    id: "team-1",
    name: "A",
    is_personal: false,
    role,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  };
}

let scope: EffectScope | null = null;

beforeEach(() => {
  window.localStorage.clear();
  setActivePinia(createPinia());
  const teams = useTeamsStore();
  teams.teams = [team("owner")];
  teams.activeTeamId = "team-1";
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
    // A dot in a name must not merge two different root/path pairs.
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

    const huge = "x".repeat(EDITOR_DRAFT_MAX_BYTES + 1);
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
  /** start opens one file with the given body and starts the draft watcher. */
  async function start(body = "base\n", etag = '"v1"') {
    getContentMock.mockResolvedValue(content(body, etag));
    const editor = useEditorStore();
    scope = effectScope();
    const drafts = scope.run(() => useEditorDrafts(() => server))!;
    await editor.openFile(server, "team-1", "volumes", "app.yaml");
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
    expect(await editor.save(tab.id, "team-1")).toBe("saved");
    await nextTick();
    expect(readEditorDraft(window.localStorage, key)).toBeNull();
    expect(drafts.prompt.value).toBeNull();
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

  it("never persists a read-only root", async () => {
    vi.useFakeTimers();
    const files = useFilesStore();
    files.roots = [{ name: "compose", path: "/c", writable: false, free_bytes: 0 }];
    files.rootName = "compose";
    getContentMock.mockResolvedValue(content("base\n"));
    const editor = useEditorStore();
    scope = effectScope();
    scope.run(() => useEditorDrafts(() => server));
    await editor.openFile(server, "team-1", "compose", "docker-compose.yaml");
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
