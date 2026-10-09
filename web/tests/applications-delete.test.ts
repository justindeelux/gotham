// Delete application (JUS-80): the confirm button stays disabled until the
// typed name matches, and the store remove calls deleteApplication once.
/* global HTMLButtonElement: readonly, HTMLInputElement: readonly, Event: readonly */
import { createPinia, setActivePinia } from "pinia";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";

vi.mock("@/features/applications/api/applications", async (importOriginal) => {
  const mod = await importOriginal<Record<string, unknown>>();
  return { ...mod, deleteApplication: vi.fn() };
});

import { deleteApplication } from "@/features/applications/api/applications";
import ApplicationDeleteDialog from "../src/features/applications/components/ApplicationDeleteDialog.vue";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const mockDelete = vi.mocked(deleteApplication);

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  mockDelete.mockReset().mockResolvedValue(undefined);
  globalThis.document.body.innerHTML = "";
});

/** modalButtons returns the teleported modal buttons by visible text. */
function modalButton(text: string): HTMLButtonElement | null {
  const buttons = [...globalThis.document.body.querySelectorAll("button")];
  return (buttons.find((button) => button.textContent?.trim() === text) ?? null) as HTMLButtonElement | null;
}

function mountDialog() {
  return mount(ApplicationDeleteDialog, {
    props: { show: true, appName: "storefront", deleting: false },
    attachTo: globalThis.document.body,
    global: { plugins: [i18n] },
  });
}

/** setModalInput types into the teleported confirmation input. */
async function setModalInput(value: string): Promise<void> {
  const input = globalThis.document.body.querySelector("input") as HTMLInputElement | null;
  expect(input).not.toBeNull();
  input!.value = value;
  input!.dispatchEvent(new Event("input", { bubbles: true }));
  await nextTick();
}

describe("ApplicationDeleteDialog", () => {
  it("keeps confirm disabled until the typed name matches", async () => {
    const wrapper = mountDialog();
    await nextTick();
    expect(modalButton("Delete")).not.toBeNull();
    expect(modalButton("Delete")!.disabled).toBe(true);
    await setModalInput("storefron");
    expect(modalButton("Delete")!.disabled).toBe(true);
    await setModalInput("storefront");
    expect(modalButton("Delete")!.disabled).toBe(false);
    wrapper.unmount();
  });

  it("emits confirm once when the name matches", async () => {
    const wrapper = mountDialog();
    await nextTick();
    await setModalInput("storefront");
    modalButton("Delete")!.click();
    await nextTick();
    expect(wrapper.emitted("confirm")?.length).toBe(1);
    wrapper.unmount();
  });
});

describe("useApplicationsStore remove", () => {
  it("calls deleteApplication once and drops the cached row", async () => {
    const store = useApplicationsStore();
    store.applicationsById["app-1"] = { id: "app-1", name: "storefront" } as never;
    await store.remove("app-1");
    expect(mockDelete).toHaveBeenCalledTimes(1);
    expect(mockDelete).toHaveBeenCalledWith("app-1");
    expect(store.applicationsById["app-1"]).toBeUndefined();
  });
});
