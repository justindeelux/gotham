// Recycle-bin dialog states (JUS-122): empty, truncated, load failure and the
// restore 409 conflict. The files API is mocked so the store and dialog run
// without axios; teleport is stubbed so the modal stays inside the wrapper.
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    listRoots: vi.fn(),
    listEntries: vi.fn(),
    listTrash: vi.fn(),
    restoreTrash: vi.fn(),
    purgeTrash: vi.fn(),
    emptyTrash: vi.fn(),
  };
});

import {
  listEntries,
  listRoots,
  listTrash,
  restoreTrash,
} from "@/features/files/api/files";
import RecycleBinDialog from "@/features/files/components/RecycleBinDialog.vue";
import type { FilesRoot, TrashEntry } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { useTeamsStore } from "@/features/teams";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const root: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 1,
};

const otherRoot: FilesRoot = {
  name: "compose",
  path: "/var/lib/gotham-agent/compose",
  writable: true,
  free_bytes: 1,
};

/** binEntry builds one recycle-bin row with sensible defaults. */
function binEntry(overrides: Partial<TrashEntry> & { id: string }): TrashEntry {
  return {
    name: "app.yaml",
    original_path: "storefront-8f2c1ab/app.yaml",
    deleted_at: "2026-10-13T10:00:00Z",
    size: 2458,
    kind: "file",
    ...overrides,
  };
}

function dialogShell() {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, {
        default: () => h(RecycleBinDialog as never, { show: true }),
      });
    },
  });
}

/** mountDialog binds a loaded store and mounts the dialog open. */
async function mountDialog(): Promise<ReturnType<typeof mount>> {
  const files = useFilesStore();
  await files.init("server-1");
  return mount(dialogShell(), {
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
}

/** buttonWith finds a button whose visible label contains `text`. */
function buttonWith(
  wrapper: ReturnType<typeof mount>,
  text: string,
): ReturnType<typeof wrapper.findAll>[number] | undefined {
  return wrapper.findAll("button").find((button) => button.text().includes(text));
}

beforeEach(() => {
  setActivePinia(createPinia());
  resetLocaleState();
  registerDiscoveredCatalogs();
  syncComposerLocale("en");
  vi.clearAllMocks();
  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = [{ id: "team-1", name: "A", is_personal: true, role: "owner" }];
  vi.mocked(listRoots).mockResolvedValue([root]);
  vi.mocked(listEntries).mockResolvedValue({
    entries: [],
    truncated: false,
    cursor: "",
  });
});

describe("recycle bin dialog", () => {
  it("shows the empty state when the bin holds nothing", async () => {
    vi.mocked(listTrash).mockResolvedValue({ entries: [], truncated: false });

    const wrapper = await mountDialog();
    await flushPromises();

    expect(wrapper.text()).toContain("The recycle bin is empty");
    // Empty bin: no footer Empty action to offer.
    expect(buttonWith(wrapper, "Empty bin")).toBeUndefined();
  });

  it("lists entries and says so when the listing is truncated", async () => {
    vi.mocked(listTrash).mockResolvedValue({
      entries: [binEntry({ id: "t1" })],
      truncated: true,
    });

    const wrapper = await mountDialog();
    await flushPromises();

    expect(wrapper.text()).toContain("app.yaml");
    expect(wrapper.text()).toContain("storefront-8f2c1ab/app.yaml");
    expect(wrapper.text()).toContain("more entries than shown");
    expect(buttonWith(wrapper, "Restore")).toBeDefined();
    expect(buttonWith(wrapper, "Empty bin")).toBeDefined();
  });

  it("surfaces a load failure with a retry", async () => {
    vi.mocked(listTrash).mockRejectedValue({ message: "boom", status: 500 });

    const wrapper = await mountDialog();
    await flushPromises();

    expect(wrapper.text()).toContain("Could not load the recycle bin");
    expect(buttonWith(wrapper, "Retry")).toBeDefined();
  });

  it("explains a restore 409 conflict inline", async () => {
    vi.mocked(listTrash).mockResolvedValue({
      entries: [binEntry({ id: "t1" })],
      truncated: false,
    });
    vi.mocked(restoreTrash).mockRejectedValue({ message: "conflict", status: 409 });

    const wrapper = await mountDialog();
    await flushPromises();

    await buttonWith(wrapper, "Restore")!.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("Restore conflict");
    expect(wrapper.text()).toContain("could not be restored");
    expect(wrapper.text()).toContain("app.yaml");
  });

  it("disables the actions when the loaded listing is from another root", async () => {
    vi.mocked(listRoots).mockResolvedValue([root, otherRoot]);
    vi.mocked(listTrash).mockResolvedValue({
      entries: [binEntry({ id: "t1" })],
      truncated: false,
    });

    const wrapper = await mountDialog();
    await flushPromises();
    expect(buttonWith(wrapper, "Restore")?.attributes("disabled")).toBeUndefined();

    // Switch root: the loaded rows no longer belong to the current scope.
    await useFilesStore().setRoot("compose");
    await flushPromises();

    expect(buttonWith(wrapper, "Restore")?.attributes("disabled")).toBeDefined();
    expect(buttonWith(wrapper, "Empty bin")?.attributes("disabled")).toBeDefined();
  });

  it("uses a count-free empty confirm when the listing is truncated", async () => {
    vi.mocked(listTrash).mockResolvedValue({
      entries: [binEntry({ id: "t1" })],
      truncated: true,
    });

    const wrapper = await mountDialog();
    await flushPromises();

    await buttonWith(wrapper, "Empty bin")!.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("including the older ones not shown");
  });
});
