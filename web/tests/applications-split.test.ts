// Component tests for the panels extracted during the JUS-24 split
// (F2-applications). Each case pins the user-visible copy, roles and emitted
// events the monoliths rendered before the split.

import { NButton } from "naive-ui";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";

import ApplicationHeader from "../src/features/applications/components/ApplicationHeader.vue";
import ApplicationOverviewTab from "../src/features/applications/components/ApplicationOverviewTab.vue";
import GothamIcon from "../src/shared/ui/GothamIcon.vue";
import type { Deployment } from "../src/features/applications/api/applications";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

const running = { id: "deploy-12345678", state: "running" } as Deployment;

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

/** mountWithI18n provides the composer every localized component requires. */
function mountWithI18n(component: unknown, options: Record<string, unknown>) {
  return mount(component as never, {
    ...(options as object),
    global: { plugins: [i18n] },
  } as never);
}

describe("ApplicationHeader", () => {
  function mountHeader(overrides = {}) {
    return mountWithI18n(ApplicationHeader, {
      props: {
        displayName: "storefront",
        appId: "app-abcdef",
        shortId: "app-abcd",
        initials: "AP",
        latest: running,
        containerStopped: false,
        activeDeploying: false,
        canRollback: true,
        acting: false,
        controlHint: null,
        containerIsRunning: true,
        ...overrides,
      },
    });
  }

  it("renders the name, id and control buttons", () => {
    const wrapper = mountHeader();
    expect(wrapper.text()).toContain("storefront");
    expect(wrapper.text()).toContain("app-abcdef");
    for (const label of ["Redeploy", "Rollback", "Stop", "Start"]) {
      expect(wrapper.findAll("button").some((button) => button.text() === label)).toBe(true);
    }
  });

  it("shows Deploying… while a deployment is in flight", () => {
    const wrapper = mountHeader({ activeDeploying: true });
    expect(wrapper.text()).toContain("Deploying…");
  });

  it("shows the stopped tag after a local stop", () => {
    const wrapper = mountHeader({ containerStopped: true, containerIsRunning: false });
    expect(wrapper.text()).toContain("stopped");
  });

  it("renders Vietnamese controls after a live switch without remounting", async () => {
    const wrapper = mountHeader();
    expect(wrapper.text()).toContain("Redeploy");
    setLocale("vi", null);
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("Triển khai lại");
    expect(wrapper.text()).toContain("Quay lui");
    expect(wrapper.text()).toContain("Dừng");
    expect(wrapper.text()).toContain("Khởi động");
    expect(wrapper.text()).not.toContain("Redeploy");
  });

  it("emits deploy, rollback and stop while running", async () => {
    const wrapper = mountHeader();
    const byText = (label: string) =>
      wrapper.findAll("button").find((button) => button.text() === label);
    await byText("Redeploy")?.trigger("click");
    await byText("Rollback")?.trigger("click");
    await byText("Stop")?.trigger("click");
    expect(wrapper.emitted("deploy")).toHaveLength(1);
    expect(wrapper.emitted("rollback")).toHaveLength(1);
    expect(wrapper.emitted("stop")).toHaveLength(1);
    // Start is disabled while the container runs; Stop is disabled once stopped.
    expect(byText("Start")?.attributes("disabled")).not.toBeUndefined();
  });

  it("emits start once stopped", async () => {
    const wrapper = mountHeader({ containerStopped: true, containerIsRunning: false });
    const start = wrapper.findAll("button").find((button) => button.text() === "Start");
    await start?.trigger("click");
    expect(wrapper.emitted("start")).toHaveLength(1);
  });

  it("gives each control its own icon and intent color", () => {
    const wrapper = mountHeader();
    const buttons = wrapper.findAllComponents(NButton);
    const byLabel = (label: string) =>
      buttons.find((button) => button.text() === label);
    // Intent colors come from the Gotham-mapped Naive theme (JUS-87): one
    // distinct type per control, so they read apart in light and dark.
    // Outline style (ghost): transparent background, colored border/text.
    expect(byLabel("Redeploy")?.props("type")).toBe("primary");
    expect(byLabel("Rollback")?.props("type")).toBe("warning");
    expect(byLabel("Stop")?.props("type")).toBe("error");
    expect(byLabel("Start")?.props("type")).toBe("success");
    for (const label of ["Redeploy", "Rollback", "Stop", "Start"]) {
      expect(byLabel(label)?.props("ghost")).toBe(true);
    }
    const icons = wrapper.findAllComponents(GothamIcon).map((icon) => icon.props("name"));
    expect(icons).toEqual(expect.arrayContaining(["refresh", "history", "stop", "play"]));
  });
});

// The flat ApplicationsPage and its ApplicationListCard were removed in PE-5:
// resources are listed on the environment page and open under nested routes.
describe("ApplicationOverviewTab", () => {
  it("renders the first-deploy empty state without a latest deployment", () => {
    const wrapper = mountWithI18n(ApplicationOverviewTab, {
      props: {
        application: null,
        latest: null,
        deployments: [],
        pipelineSteps: [],
        descColumns: 2,
        acting: false,
      },
    });
    expect(wrapper.text()).toContain("No deployments yet");
    expect(wrapper.text()).toContain("Queue the first deploy to start the pipeline.");
    expect(wrapper.text()).toContain("Recent deployments");
    expect(wrapper.text()).toContain("No deployments recorded for this application.");
  });

  it("renders the Vietnamese empty state after a live switch", async () => {
    const wrapper = mountWithI18n(ApplicationOverviewTab, {
      props: {
        application: null,
        latest: null,
        deployments: [],
        pipelineSteps: [],
        descColumns: 2,
        acting: false,
      },
    });
    setLocale("vi", null);
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("Chưa có đợt triển khai nào");
    expect(wrapper.text()).toContain("Các đợt triển khai gần đây");
  });
});
