// Component tests for the panels extracted during the JUS-24 split
// (F2-applications). Each case pins the user-visible copy, roles and emitted
// events the monoliths rendered before the split.

import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import ApplicationHeader from "../src/features/applications/components/ApplicationHeader.vue";
import ApplicationListCard from "../src/features/applications/components/ApplicationListCard.vue";
import ApplicationOverviewTab from "../src/features/applications/components/ApplicationOverviewTab.vue";
import type { Application, Deployment } from "../src/features/applications/api/applications";

const running = { id: "deploy-12345678", state: "running" } as Deployment;

describe("ApplicationHeader", () => {
  function mountHeader(overrides = {}) {
    return mount(ApplicationHeader, {
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
});

describe("ApplicationListCard", () => {
  it("renders rows with an Open button per application", () => {
    const wrapper = mount(ApplicationListCard, {
      props: {
        knownApps: [
          { id: "app-1", name: "storefront", branch: "main" },
        ] as Application[],
        listLoading: false,
        listError: null,
        openById: "",
      },
    });
    expect(wrapper.text()).toContain("storefront");
    expect(wrapper.text()).toContain("Open");
  });

  it("shows the empty state with the open-by-id field", async () => {
    const wrapper = mount(ApplicationListCard, {
      props: { knownApps: [], listLoading: false, listError: null, openById: "" },
    });
    expect(wrapper.text()).toContain("No applications yet");
    expect(wrapper.text()).toContain(
      "Create the first application to get a deployment pipeline",
    );
    const field = wrapper.find('[aria-label="Application ID"]');
    expect(field.exists()).toBe(true);
    const input = wrapper.find('input[placeholder="Paste an application ID to open it"]');
    expect(input.exists()).toBe(true);
  });

  it("emits open with the trimmed id", async () => {
    const wrapper = mount(ApplicationListCard, {
      props: { knownApps: [], listLoading: false, listError: null, openById: "  app-9  " },
    });
    const openButton = wrapper.findAll("button").find((button) => button.text() === "Open");
    await openButton?.trigger("click");
    expect(wrapper.emitted("open")).toEqual([["app-9"]]);
  });

  it("renders the list error with the recovery hint", () => {
    const wrapper = mount(ApplicationListCard, {
      props: { knownApps: [], listLoading: false, listError: "boom", openById: "" },
    });
    expect(wrapper.text()).toContain("boom");
    expect(wrapper.text()).toContain("Create an application to get started");
  });
});

describe("ApplicationOverviewTab", () => {
  it("renders the first-deploy empty state without a latest deployment", () => {
    const wrapper = mount(ApplicationOverviewTab, {
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
});
