// Permissions dialog (JUS-113 fix round 1, simplified by JUS-131): the grid is
// inert while the octal field is invalid (it must not rewrite what the user
// typed), Apply is disabled when the mode is unchanged or a directory would
// lose owner execute, and the octal field now sits BELOW the bits grid.
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
    chmodEntry: vi.fn(),
    listRoots: vi.fn(),
    listEntries: vi.fn(),
  };
});

import { chmodEntry, listEntries, listRoots } from "@/features/files/api/files";
import PermissionsDialog from "@/features/files/components/PermissionsDialog.vue";
import viCatalog from "@/features/files/locales/vi";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
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

/** entry builds one listing row with sensible defaults. */
function entry(overrides: Partial<FilesEntry> & { name: string }): FilesEntry {
  return {
    kind: "file",
    size: 0,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
    ...overrides,
  };
}

function dialogShell(props: Record<string, unknown>) {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, {
        default: () => h(PermissionsDialog as never, props),
      });
    },
  });
}

function mountDialog(target: FilesEntry) {
  return mount(dialogShell({ show: true, entry: target }), {
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
}

/** applyButton returns the footer Apply button, found by its label. */
function applyButton(wrapper: ReturnType<typeof mountDialog>) {
  return wrapper.findAll("button").find((b) => b.text().includes("Apply"));
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

async function initStore(): Promise<void> {
  const store = useFilesStore();
  await store.init("server-1");
}

describe("permissions dialog", () => {
  it("disables the grid and keeps the typed octal while invalid", async () => {
    await initStore();
    const wrapper = mountDialog(entry({ name: "app.yaml" }));
    await flushPromises();

    const input = wrapper.find(".octal input");
    await input.setValue("99");
    await flushPromises();

    const checkboxes = wrapper.findAll(".n-checkbox");
    expect(checkboxes.length).toBe(9);
    for (const box of checkboxes) {
      expect(box.classes()).toContain("n-checkbox--disabled");
    }
    // The invalid text is untouched and Apply stays disabled.
    expect((input.element as { value: string }).value).toBe("99");
    expect(applyButton(wrapper)?.attributes("disabled")).toBeDefined();
    expect(vi.mocked(chmodEntry)).not.toHaveBeenCalled();
  });

  it("enables Apply only when the mode changes", async () => {
    await initStore();
    const wrapper = mountDialog(entry({ name: "app.yaml", mode: 0o100644 }));
    await flushPromises();

    // The dialog opens on the entry's current mode: nothing to apply yet.
    expect(applyButton(wrapper)?.attributes("disabled")).toBeDefined();

    await wrapper.find(".octal input").setValue("600");
    await flushPromises();
    expect(applyButton(wrapper)?.attributes("disabled")).toBeUndefined();
  });

  it("refuses a directory that would lose owner execute", async () => {
    await initStore();
    const wrapper = mountDialog(
      entry({ name: "uploads", kind: "directory", mode: 0o040755 }),
    );
    await flushPromises();

    await wrapper.find(".octal input").setValue("666");
    await flushPromises();
    expect(wrapper.text()).toContain(
      "A directory must keep owner execute so it stays navigable.",
    );
    expect(applyButton(wrapper)?.attributes("disabled")).toBeDefined();

    // Owner execute present: valid again.
    await wrapper.find(".octal input").setValue("700");
    await flushPromises();
    expect(applyButton(wrapper)?.attributes("disabled")).toBeUndefined();
  });

  it("puts the octal field below the bits grid and drops the extra copy", async () => {
    await initStore();
    const wrapper = mountDialog(entry({ name: "app.yaml" }));
    await flushPromises();

    // Bits first, then the octal field: in document order the grid precedes
    // the `.octal` input the user types in.
    const html = wrapper.html();
    expect(html.indexOf("perms-grid")).toBeGreaterThanOrEqual(0);
    expect(html.indexOf("octal")).toBeGreaterThan(html.indexOf("perms-grid"));

    // JUS-131: the explanatory copy and the labelled "current mode" line are
    // gone, while the read-only symbolic + octal mode stays visible.
    const text = wrapper.text();
    expect(text).not.toContain("Three octal digits");
    expect(text).not.toContain("Special bits");
    expect(text).not.toContain("keeps its type");
    expect(text).not.toContain("Current mode");
    expect(text).toContain("-rw-r--r-- · 644");
    // en/vi keep one short label for the octal field.
    expect(viCatalog.permissions.octal).toBe("Bát phân");
  });
});
