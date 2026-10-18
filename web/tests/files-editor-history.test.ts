// File version history (JUS-224): the read-only diff tab in the editor store and
// the history dialog's list/diff/restore/delete wiring. The files API is mocked
// so the store and dialog run without axios.
/* global document:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider, NPopconfirm } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";

vi.mock("@/features/files/composables/useCodeMirror", () => ({
  createEditorSession: vi.fn(async () => ({
    getDoc: () => "",
    setDoc: vi.fn(),
    setLanguage: vi.fn(),
    setSettings: vi.fn(),
    setReadOnly: vi.fn(),
    focus: vi.fn(),
    requestMeasure: vi.fn(),
    openSearch: vi.fn(),
    goToLine: vi.fn(),
    destroy: vi.fn(),
  })),
}));

vi.mock("@/features/files/composables/useFileDownload", () => ({
  useFileDownload: () => ({ pending: { value: false }, download: vi.fn() }),
}));

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    getContent: vi.fn(),
    getFileVersion: vi.fn(),
    listFileVersions: vi.fn(),
    deleteFileVersion: vi.fn(),
    restoreFileVersion: vi.fn(),
    putContent: vi.fn(),
    createFile: vi.fn(),
    deleteEntry: vi.fn(),
  };
});

import {
  deleteFileVersion,
  getContent,
  getFileVersion,
  listFileVersions,
} from "@/features/files/api/files";
import FilesEditor from "@/features/files/components/FilesEditor.vue";
import EditorHistoryDialog from "@/features/files/components/EditorHistoryDialog.vue";
import editorEn from "@/features/files/locales/editorEn";
import type { FilesVersion } from "@/features/files/schemas/files";
import { useEditorStore } from "@/features/files/stores/editor";
import type { EditorTab } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import { useTeamsStore } from "@/features/teams";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const version = (id: string, createdAt: string, size = 12): FilesVersion => ({
  id,
  created_at: createdAt,
  size,
  etag: `"${id}"`,
});

const versions: FilesVersion[] = [
  version("v2", "2026-10-18T10:00:00Z", 24),
  version("v1", "2026-10-17T09:00:00Z", 12),
];

/** dialogShell mounts the dialog open under a message provider. */
function dialogShell(dirty = false) {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, {
        default: () =>
          h(EditorHistoryDialog as never, {
            show: true,
            serverId: "srv",
            teamId: "team-1",
            root: "volumes",
            path: "a.txt",
            fileName: "a.txt",
            dirty,
          }),
      });
    },
  });
}

/** positiveClick fires a popconfirm's confirm action. */
function positiveClick(component: { props: (_name: string) => () => void }): void {
  component.props("onPositiveClick")();
}

beforeEach(() => {
  setActivePinia(createPinia());
  resetLocaleState();
  syncComposerLocale("en");
  i18n.global.mergeLocaleMessage("en", { files: { editor: editorEn } });
  vi.clearAllMocks();
  vi.mocked(listFileVersions).mockResolvedValue(versions);
});

describe("editor history diff tab", () => {
  it("opens one read-only diff tab with the stored base and live body", async () => {
    vi.mocked(getFileVersion).mockResolvedValue({
      version: versions[0],
      content: "old body",
    });
    vi.mocked(getContent).mockResolvedValue({
      content: "current body",
      etag: '"v2"',
      sha256: "x",
      size: 12,
      mode: 0o100644,
    });
    const editor = useEditorStore();

    await editor.openDiff("srv", "team-1", "volumes", "a.txt", "v1", "a.txt · now");

    expect(editor.tabs).toHaveLength(1);
    const tab = editor.tabs[0];
    expect(tab.kind).toBe("diff");
    expect(tab.status).toBe("ready");
    expect(tab.name).toBe("a.txt · now");
    expect(tab.diff).toEqual({
      versionId: "v1",
      base: "old body",
      current: "current body",
    });
    expect(editor.isDirty(tab)).toBe(false);
    expect(await editor.save(tab.id, "team-1")).toBe("blocked");
    expect(await editor.saveAsCopy(tab.id, "team-1", "copy.txt")).toBe("blocked");
  });

  it("reuses the diff tab of the same revision and opens another for a new one", async () => {
    vi.mocked(getFileVersion).mockResolvedValue({
      version: versions[0],
      content: "old body",
    });
    vi.mocked(getContent).mockResolvedValue({
      content: "current body",
      etag: '"v2"',
      sha256: "x",
      size: 12,
      mode: 0o100644,
    });
    const editor = useEditorStore();
    await editor.openDiff("srv", "team-1", "volumes", "a.txt", "v1", "v1 · time");
    const first = editor.tabs[0];

    // A reused diff re-reads both sides, so the right side is the current file.
    vi.mocked(getFileVersion).mockResolvedValue({
      version: versions[0],
      content: "old body",
    });
    vi.mocked(getContent).mockResolvedValue({
      content: "newer current",
      etag: '"v3"',
      sha256: "y",
      size: 13,
      mode: 0o100644,
    });
    await editor.openDiff("srv", "team-1", "volumes", "a.txt", "v1", "v1 · time");
    expect(editor.tabs).toHaveLength(1);
    expect(editor.activeId).toBe(first.id);
    expect(first.diff?.current).toBe("newer current");

    await editor.openDiff("srv", "team-1", "volumes", "a.txt", "v2", "v2 · time");
    expect(editor.tabs).toHaveLength(2);
    expect(editor.activeTab?.diff?.versionId).toBe("v2");
  });

  it("shows an error tab when the live file cannot be read", async () => {
    vi.mocked(getFileVersion).mockResolvedValue({
      version: versions[0],
      content: "old body",
    });
    vi.mocked(getContent).mockRejectedValue(new Error("gone"));
    const editor = useEditorStore();

    await editor.openDiff("srv", "team-1", "volumes", "a.txt", "v1", "v1 · time");

    expect(editor.tabs[0].status).toBe("error");
    expect(editor.tabs[0].diff).toBeUndefined();
  });
});

describe("history in the editor", () => {
  it("shows the revision count and opens the dialog from the toolbar", async () => {
    const teams = useTeamsStore();
    teams.teams = [
      {
        id: "team-1",
        name: "A",
        is_personal: false,
        role: "owner",
        created_at: "2026-10-01T00:00:00Z",
        updated_at: "2026-10-01T00:00:00Z",
      },
    ];
    teams.activeTeamId = "team-1";
    const files = useFilesStore();
    files.roots = [{ name: "volumes", path: "/v", writable: true, free_bytes: 1 }];
    files.rootName = "volumes";

    const editor = useEditorStore();
    editor.serverId = "srv";
    const tab: EditorTab = {
      id: "t1",
      kind: "file",
      root: "volumes",
      path: "a.txt",
      name: "a.txt",
      doc: "start",
      original: "start",
      etag: '"v2"',
      eol: "\n",
      bom: false,
      size: 6,
      mode: 0o100644,
      truncated: false,
      language: "plain",
      languageManual: false,
      status: "ready",
      error: "",
      saving: false,
    };
    editor.tabs = [tab];
    editor.activeId = tab.id;

    const wrapper = mount(
      defineComponent({
        render: () =>
          h(NMessageProvider, null, {
            default: () => h(FilesEditor, { serverId: "srv" }),
          }),
      }),
      { global: { plugins: [i18n], stubs: { teleport: true } }, attachTo: document.body },
    );
    await flushPromises();

    expect(wrapper.text()).toContain("2 versions");
    expect(i18n.global.t("files.editor.historyCount", 1)).toBe("1 version");
    const history = wrapper
      .findAll("button")
      .find((button) => button.text().includes("History"));
    expect(history).toBeDefined();
    await history!.trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("File history — a.txt");
    expect(vi.mocked(listFileVersions)).toHaveBeenCalledWith(
      "srv",
      "team-1",
      "volumes",
      "a.txt",
    );

    // Opening the diff uses a localized title, not the raw locale default.
    vi.mocked(getFileVersion).mockResolvedValue({
      version: versions[0],
      content: "old",
    });
    vi.mocked(getContent).mockResolvedValue({
      content: "current",
      etag: '"v2"',
      sha256: "x",
      size: 7,
      mode: 0o100644,
    });
    const diff = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Diff"));
    await diff!.trigger("click");
    await flushPromises();
    expect(editor.activeTab?.kind).toBe("diff");
    expect(editor.activeTab?.name).toMatch(/^a\.txt · /);
    wrapper.unmount();
  });
});

describe("history dialog", () => {
  it("lists versions newest first with a size and diff action", async () => {
    const wrapper = mount(dialogShell(), {
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    const rows = wrapper.findAll("tbody tr");
    expect(rows).toHaveLength(2);
    expect(rows[0].find("td").attributes("title")).toBe("2026-10-18T10:00:00Z");
    expect(rows[1].find("td").attributes("title")).toBe("2026-10-17T09:00:00Z");

    const diff = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Diff"));
    expect(diff).toBeDefined();
    await diff!.trigger("click");
    expect(wrapper.findComponent(EditorHistoryDialog).emitted("diff")?.[0]?.[0]).toMatchObject({
      id: "v2",
    });
    wrapper.unmount();
  });

  it("shows the empty state when nothing was saved yet", async () => {
    vi.mocked(listFileVersions).mockResolvedValue([]);
    const wrapper = mount(dialogShell(), {
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    expect(wrapper.text()).toContain("No saved versions yet");
    wrapper.unmount();
  });

  it("emits restore and warns about unsaved changes", async () => {
    const wrapper = mount(dialogShell(true), {
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    const restore = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Restore"));
    expect(restore).toBeDefined();
    await restore!.trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("This file has unsaved changes");

    positiveClick(wrapper.findAllComponents(NPopconfirm)[0]);
    await flushPromises();
    expect(wrapper.findComponent(EditorHistoryDialog).emitted("restore")?.[0]?.[0]).toMatchObject({
      id: "v2",
    });
    wrapper.unmount();
  });

  it("deletes one revision and reloads the list", async () => {
    vi.mocked(deleteFileVersion).mockResolvedValue(undefined);
    const wrapper = mount(dialogShell(), {
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    positiveClick(wrapper.findAllComponents(NPopconfirm)[1]);
    await flushPromises();

    expect(vi.mocked(deleteFileVersion)).toHaveBeenCalledWith(
      "srv",
      "team-1",
      "volumes",
      "a.txt",
      "v2",
    );
    expect(vi.mocked(listFileVersions)).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });
});
