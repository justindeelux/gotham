// DeployLogs branch: a finished deployment reads the persisted build log
// (JUS-84) while an in-flight one keeps the realtime stream.
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import DeployLogs from "@/features/applications/components/DeployLogs.vue";
import type { Deployment } from "@/features/applications/api/applications";
import {
  getDeploymentBuildLog,
} from "@/features/applications/api/applications";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

vi.mock("@/features/applications/api/applications", () => ({
  deployChannel: (serverId: string, deploymentId: string) => `logs:${serverId}:${deploymentId}`,
  isActiveDeployment: (deployment: { state: string }) =>
    ["queued", "cloning", "building", "pushing", "starting"].includes(deployment.state),
  getDeploymentBuildLog: vi.fn(),
}));

const mockedBuildLog = vi.mocked(getDeploymentBuildLog);

const LiveStub = { template: "<div data-testid='live-stream' />" };

function deployment(state: Deployment["state"]): Deployment {
  return {
    id: "11111111-2222-3333-4444-555555555555",
    application_id: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
    kind: "deploy",
    state,
    image_tag: "",
    registry_image: "",
    digest: "",
    error: "",
    attempt: 1,
    container_id: "",
    rollback_from: "",
    started_at: null,
    finished_at: null,
    created_at: "",
    updated_at: "",
  };
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  mockedBuildLog.mockResolvedValue("line one\nline two");
});

afterEach(() => {
  globalThis.document.body.innerHTML = "";
  vi.restoreAllMocks();
});

function mountLogs(dep: Deployment | null) {
  return mount(DeployLogs, {
    props: { serverId: "node-1", deployment: dep },
    attachTo: globalThis.document.body,
    global: { plugins: [i18n], stubs: { LogViewer: LiveStub } },
  });
}

describe("DeployLogs", () => {
  it("streams an in-flight deployment without reading the stored log", async () => {
    const wrapper = mountLogs(deployment("building"));
    await flushPromises();
    expect(wrapper.find("[data-testid='live-stream']").exists()).toBe(true);
    expect(wrapper.find(".deploy-logs__stored").exists()).toBe(false);
    expect(mockedBuildLog).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("shows the stored log of a finished deployment", async () => {
    const wrapper = mountLogs(deployment("failed"));
    await flushPromises();
    expect(mockedBuildLog).toHaveBeenCalledWith(
      "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
      "11111111-2222-3333-4444-555555555555",
    );
    expect(wrapper.find("[data-testid='live-stream']").exists()).toBe(false);
    const stored = wrapper.find(".deploy-logs__stored");
    expect(stored.exists()).toBe(true);
    expect(stored.text()).toContain("line one");
    wrapper.unmount();
  });

  it("names the missing stored log instead of streaming nothing", async () => {
    mockedBuildLog.mockResolvedValue("");
    const wrapper = mountLogs(deployment("running"));
    await flushPromises();
    expect(wrapper.find("[data-testid='live-stream']").exists()).toBe(false);
    expect(wrapper.find(".deploy-logs__stored").text()).toContain(
      "No stored log for this deployment.",
    );
    wrapper.unmount();
  });

  it("falls back to the missing-log copy when the read fails", async () => {
    mockedBuildLog.mockRejectedValue(new Error("gone"));
    const wrapper = mountLogs(deployment("failed"));
    await flushPromises();
    expect(wrapper.find(".deploy-logs__stored").text()).toContain(
      "No stored log for this deployment.",
    );
    wrapper.unmount();
  });
});
