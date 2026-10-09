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

function deployment(state: Deployment["state"], finishedAt: string | null = null): Deployment {
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
    finished_at: finishedAt,
    created_at: "",
    updated_at: "",
  };
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.useFakeTimers();
  mockedBuildLog.mockResolvedValue("line one\nline two");
});

afterEach(() => {
  globalThis.document.body.innerHTML = "";
  vi.useRealTimers();
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
    const hourAgo = new Date(Date.now() - 3_600_000).toISOString();
    const wrapper = mountLogs(deployment("running", hourAgo));
    await flushPromises();
    expect(wrapper.find("[data-testid='live-stream']").exists()).toBe(false);
    expect(wrapper.find(".deploy-logs__stored").text()).toContain(
      "No stored log for this deployment.",
    );
    wrapper.unmount();
  });

  it("shows a loading state while the stored log is being read", async () => {
    let resolve!: (_value: string) => void;
    mockedBuildLog.mockReturnValue(
      new Promise<string>((res) => {
        resolve = res;
      }),
    );
    const wrapper = mountLogs(deployment("failed"));
    await flushPromises();
    // Loading, not the generic empty copy and not the missing-log copy.
    expect(wrapper.text()).toContain("Loading the stored log");
    expect(wrapper.find(".deploy-logs__stored").exists()).toBe(false);
    resolve("late lines");
    await flushPromises();
    expect(wrapper.find(".deploy-logs__stored").text()).toContain("late lines");
    wrapper.unmount();
  });

  it("retries an empty read of a just-finished deployment before settling", async () => {
    mockedBuildLog.mockResolvedValue("");
    const wrapper = mountLogs(deployment("failed"));
    await flushPromises();
    expect(wrapper.text()).toContain("Loading the stored log");
    await vi.advanceTimersByTimeAsync(500);
    await flushPromises();
    await vi.advanceTimersByTimeAsync(500);
    await flushPromises();
    expect(wrapper.find(".deploy-logs__stored").text()).toContain(
      "No stored log for this deployment.",
    );
    expect(mockedBuildLog).toHaveBeenCalledTimes(3);
    wrapper.unmount();
  });

  it("reads an old deployment's empty log once without retrying", async () => {
    mockedBuildLog.mockResolvedValue("");
    const hourAgo = new Date(Date.now() - 3_600_000).toISOString();
    const wrapper = mountLogs(deployment("failed", hourAgo));
    await flushPromises();
    expect(wrapper.find(".deploy-logs__stored").text()).toContain(
      "No stored log for this deployment.",
    );
    expect(mockedBuildLog).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("shows an error state with a retry button when the read fails", async () => {
    mockedBuildLog.mockRejectedValueOnce(new Error("gone"));
    const wrapper = mountLogs(deployment("failed"));
    await flushPromises();
    expect(wrapper.find(".deploy-logs__error").text()).toContain(
      "Could not load the stored log.",
    );
    const retry = wrapper.find(".deploy-logs__retry");
    expect(retry.exists()).toBe(true);
    await retry.trigger("click");
    await flushPromises();
    expect(wrapper.find(".deploy-logs__stored").text()).toContain("line one");
    wrapper.unmount();
  });
});
