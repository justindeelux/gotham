/* global HTMLButtonElement: readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  applicationContainerAction,
  listApplicationContainers,
} from "@/features/applications/api/containers";
import type { ApplicationContainer } from "@/features/applications/api/containers";
import ApplicationContainersTab from "@/features/applications/components/ApplicationContainersTab.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

vi.mock("naive-ui", async (importOriginal) => {
  const original = await importOriginal<typeof import("naive-ui")>();
  return {
    ...original,
    useMessage: () => ({ success: vi.fn(), error: vi.fn(), warning: vi.fn() }),
  };
});

vi.mock("@/features/applications/api/containers", () => ({
  listApplicationContainers: vi.fn(),
  applicationContainerAction: vi.fn(),
}));

const storeState = vi.hoisted(() => ({
  fetchApplication: vi.fn(async () => null),
}));

vi.mock("@/features/applications/stores/applications", () => ({
  useApplicationsStore: () => ({
    fetchApplication: storeState.fetchApplication,
  }),
}));

const mockList = vi.mocked(listApplicationContainers);
const mockAction = vi.mocked(applicationContainerAction);

const composeRows: ApplicationContainer[] = [
  {
    id: "web",
    name: "shop-web-1",
    service: "web",
    image: "shop:1",
    state: "running",
    status: "Up 2 minutes",
  },
  {
    id: "db",
    name: "shop-db-1",
    service: "db",
    image: "postgres:16",
    state: "exited",
    status: "Exited (0) 1 hour ago",
  },
];

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  mockList.mockReset().mockResolvedValue(composeRows);
  mockAction.mockReset().mockResolvedValue("ok");
  storeState.fetchApplication.mockReset().mockResolvedValue(null);
  globalThis.document.body.innerHTML = "";
});

function mountTab(canWrite = true) {
  return mount(ApplicationContainersTab, {
    props: { appId: "app-1", canWrite },
    attachTo: globalThis.document.body,
    global: { plugins: [createPinia(), i18n] },
  });
}

function buttonByText(text: string): HTMLButtonElement | undefined {
  const buttons = [...globalThis.document.body.querySelectorAll("button")];
  return buttons.find(
    (button) => button.textContent?.trim() === text,
  ) as HTMLButtonElement | undefined;
}

describe("ApplicationContainersTab", () => {
  it("renders one row per compose service with state and uptime", async () => {
    const wrapper = mountTab();
    await flushPromises();

    expect(wrapper.text()).toContain("shop-web-1");
    expect(wrapper.text()).toContain("shop-db-1");
    expect(wrapper.text()).toContain("postgres:16");
    expect(wrapper.text()).toContain("Up 2 minutes");
    expect(wrapper.text()).toContain("running");
    expect(wrapper.text()).toContain("exited");

    wrapper.unmount();
  });

  it("shows start for a stopped row and stop and restart for a running row", async () => {
    const wrapper = mountTab();
    await flushPromises();

    expect(buttonByText("Start")).toBeDefined();
    expect(buttonByText("Stop")).toBeDefined();
    expect(buttonByText("Restart")).toBeDefined();

    wrapper.unmount();
  });

  it("starts a stopped container and refreshes the row without a reload", async () => {
    mockList
      .mockResolvedValueOnce(composeRows)
      .mockResolvedValueOnce([
        composeRows[0],
        { ...composeRows[1], state: "running", status: "Up 1 second" },
      ]);
    const wrapper = mountTab();
    await flushPromises();

    buttonByText("Start")!.dispatchEvent(
      new globalThis.MouseEvent("click", { bubbles: true }),
    );
    await flushPromises();

    expect(mockAction).toHaveBeenCalledWith("app-1", "db", "start");
    expect(mockList).toHaveBeenCalledTimes(2);
    expect(storeState.fetchApplication).toHaveBeenCalledWith("app-1");
    expect(wrapper.text()).toContain("Up 1 second");
    expect(wrapper.text()).not.toContain("Exited (0) 1 hour ago");

    wrapper.unmount();
  });

  it("asks before stopping and only then calls the action", async () => {
    const wrapper = mountTab();
    await flushPromises();

    buttonByText("Stop")!.dispatchEvent(
      new globalThis.MouseEvent("click", { bubbles: true }),
    );
    await flushPromises();

    expect(mockAction).not.toHaveBeenCalled();
    const popover = globalThis.document.body.querySelector(".n-popconfirm");
    expect(popover, "expected the stop confirm popover").not.toBeNull();
    const positive = [...popover!.querySelectorAll("button")].find(
      (button) => button.textContent?.trim() === "Stop",
    );
    positive!.dispatchEvent(
      new globalThis.MouseEvent("click", { bubbles: true }),
    );
    await flushPromises();

    expect(mockAction).toHaveBeenCalledWith("app-1", "web", "stop");

    wrapper.unmount();
  });

  it("hides container actions from a read-only member", async () => {
    const wrapper = mountTab(false);
    await flushPromises();

    expect(buttonByText("Start")).toBeUndefined();
    expect(buttonByText("Stop")).toBeUndefined();
    expect(buttonByText("Restart")).toBeUndefined();

    wrapper.unmount();
  });

  it("renders Vietnamese labels and no raw keys after a live switch", async () => {
    const wrapper = mountTab();
    await flushPromises();

    setLocale("vi", null);
    await flushPromises();

    expect(wrapper.text()).toContain("Container");
    expect(wrapper.text()).toContain("Khởi động");
    expect(wrapper.text()).toContain("Dừng");
    expect(wrapper.text()).not.toContain("applications.containers");
    expect(buttonByText("Start")).toBeUndefined();

    wrapper.unmount();
  });
});
