import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";

import type { Deployment } from "@/features/applications/api/applications";
import ApplicationLogsTab from "@/features/applications/components/ApplicationLogsTab.vue";
import DeploymentCommitCard from "@/features/applications/components/DeploymentCommitCard.vue";
import {
  commitSubject,
  commitUrl,
  hasCommitSha,
  repoPath,
  shortCommitSha,
} from "@/features/applications/utils/deploymentCommit";
import en from "@/features/applications/locales/en";
import viCatalog from "@/features/applications/locales/vi";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

const { mockCopy } = vi.hoisted(() => ({ mockCopy: vi.fn() }));

vi.mock("@/shared/composables/useCopyText", () => ({
  useCopyText: () => ({ copyText: mockCopy }),
}));

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  mockCopy.mockClear();
});

/** deployment builds one row with commit metadata. */
function deployment(overrides: Partial<Deployment> = {}): Deployment {
  return {
    id: "deploy-12345678",
    application_id: "app-1",
    kind: "deploy",
    state: "running",
    image_tag: "registry.internal/app:42",
    registry_image: "registry.internal/app",
    digest: "",
    error: "",
    attempt: 1,
    container_id: "ctr-9",
    rollback_from: "",
    commit_sha: "0123456789abcdef0123456789abcdef01234567",
    commit_message: "Add checkout flow\n\nLonger body.",
    commit_author: "Ada",
    committed_at: new Date(Date.now() - 60_000).toISOString(),
    started_at: "2026-09-01T10:00:00Z",
    finished_at: null,
    created_at: "2026-09-01T10:00:00Z",
    updated_at: "2026-09-01T10:00:00Z",
    ...overrides,
  } as Deployment;
}

/** mountCard renders the commit card inside a message provider. */
function mountCard(row: Deployment | null, repo = "owner/repo", cloneUrl = "") {
  const Harness = defineComponent({
    setup() {
      return () =>
        h(DeploymentCommitCard, { deployment: row, repo, cloneUrl });
    },
  });
  return mount(
    {
      render: () => h(NMessageProvider, null, { default: () => h(Harness) }),
    },
    { global: { plugins: [i18n] } },
  );
}

describe("deploymentCommit helpers", () => {
  it("shortens, subjects and detects commit metadata", () => {
    expect(shortCommitSha("0123456789abcdef")).toBe("0123456");
    expect(commitSubject("Add checkout flow\n\nBody")).toBe("Add checkout flow");
    expect(commitSubject("")).toBe("");
    expect(hasCommitSha("abc")).toBe(true);
    expect(hasCommitSha("")).toBe(false);
    expect(hasCommitSha(undefined)).toBe(false);
  });

  it("links github.com remotes and nothing else", () => {
    expect(commitUrl("owner/repo", "", "abc1234")).toBe(
      "https://github.com/owner/repo/commit/abc1234",
    );
    expect(commitUrl("", "https://github.com/owner/repo.git", "abc1234")).toBe(
      "https://github.com/owner/repo/commit/abc1234",
    );
    expect(commitUrl("", "git@github.com:owner/repo.git", "abc1234")).toBe(
      "https://github.com/owner/repo/commit/abc1234",
    );
    expect(commitUrl("", "https://gitlab.com/owner/repo.git", "abc1234")).toBe("");
    expect(commitUrl("", "", "abc1234")).toBe("");
    expect(commitUrl("owner/repo", "", "")).toBe("");
    expect(repoPath("owner/repo.git", "")).toBe("owner/repo");
  });

  it("ignores the repo fallback for non-GitHub remotes", () => {
    expect(commitUrl("owner/repo", "https://gitlab.com/owner/repo.git", "abc1234")).toBe("");
    expect(commitUrl("owner/repo", "https://notgithub.com/owner/repo.git", "abc1234")).toBe("");
    expect(commitUrl("owner/repo", "https://example.com/github.com/owner/repo", "abc1234")).toBe("");
    expect(repoPath("owner/repo", "https://gitlab.com/owner/repo.git")).toBe("");
  });

  it("rejects lookalike github.com hosts", () => {
    expect(repoPath("", "https://notgithub.com/owner/repo.git")).toBe("");
    expect(repoPath("", "https://github.com.evil.com/owner/repo.git")).toBe("");
    expect(repoPath("", "https://example.com/github.com/owner/repo")).toBe("");
    expect(repoPath("", "https://example.com/a/b")).toBe("");
    expect(commitUrl("", "https://notgithub.com/owner/repo.git", "abc1234")).toBe("");
  });

  it("accepts www and credentialed github.com remotes", () => {
    const linked = "https://github.com/owner/repo/commit/abc1234";
    expect(commitUrl("", "https://www.github.com/owner/repo", "abc1234")).toBe(linked);
    expect(commitUrl("", "https://user:pass@github.com/owner/repo.git", "abc1234")).toBe(linked);
    expect(commitUrl("", "ssh://git@github.com/owner/repo.git", "abc1234")).toBe(linked);
    expect(repoPath("", "github.com/owner/repo")).toBe("owner/repo");
  });

  it("rejects non-hex and malformed shas", () => {
    expect(commitUrl("owner/repo", "", "x?y")).toBe("");
    expect(commitUrl("owner/repo", "", "../abc1234")).toBe("");
    expect(commitUrl("owner/repo", "", "javascript:alert(1)")).toBe("");
    expect(commitUrl("owner/repo", "", "abc")).toBe("");
    expect(commitUrl("owner/repo", "", "0".repeat(65))).toBe("");
    expect(commitUrl("owner/repo", "", "zzzzz00")).toBe("");
    expect(commitUrl("owner/repo", "", "ABCDEF1234567")).toBe(
      "https://github.com/owner/repo/commit/ABCDEF1234567",
    );
  });

  it("treats whitespace-only shas as empty", () => {
    expect(hasCommitSha("   ")).toBe(false);
    expect(hasCommitSha("  \n ")).toBe(false);
  });

  it("keeps en/vi commit keys in parity", () => {
    expect(checkCatalogParity(en, viCatalog)).toEqual([]);
  });
});

describe("DeploymentCommitCard", () => {
  it("hides the card when commit_sha is empty", () => {
    const wrapper = mountCard(deployment({ commit_sha: "" }));
    expect(wrapper.find('[data-testid="commit-card"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("hides the card for whitespace-only shas", () => {
    const wrapper = mountCard(deployment({ commit_sha: "   " }));
    expect(wrapper.find('[data-testid="commit-card"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("renders script and javascript: payloads as inert text", () => {
    const wrapper = mountCard(
      deployment({ commit_message: "<script>alert(1)</script>\njavascript:alert(1)" }),
    );
    const card = wrapper.find('[data-testid="commit-card"]');
    expect(card.exists()).toBe(true);
    expect(card.text()).toContain("<script>alert(1)</script>");
    expect(card.text()).toContain("javascript:alert(1)");
    expect(wrapper.find("script").exists()).toBe(false);
    expect(card.html()).toContain("&lt;script&gt;");
    const hrefs = wrapper.findAll("a").map((link) => link.attributes("href") ?? "");
    expect(hrefs.length).toBeGreaterThan(0);
    expect(hrefs.every((href) => href.startsWith("https://github.com/"))).toBe(true);
    wrapper.unmount();
  });

  it("caps long commit bodies behind a scrollable message block", () => {
    const wrapper = mountCard(deployment({ commit_message: `line 1\n${"body\n".repeat(200)}` }));
    const message = wrapper.find('[data-testid="commit-message"]');
    expect(message.exists()).toBe(true);
    expect(message.classes()).toContain("commit-message");
    wrapper.unmount();
  });

  it("renders short sha, subject, author and relative time", () => {
    const wrapper = mountCard(deployment());
    const card = wrapper.find('[data-testid="commit-card"]');
    expect(card.exists()).toBe(true);
    expect(card.text()).toContain("0123456");
    expect(card.text()).toContain("Add checkout flow");
    expect(card.text()).toContain("Ada");
    expect(card.text()).toContain("ago");
    wrapper.unmount();
  });

  it("links the commit on GitHub when the repo is known", () => {
    const wrapper = mountCard(deployment());
    const link = wrapper.find('[data-testid="commit-card"] a');
    expect(link.exists()).toBe(true);
    expect(link.attributes("href")).toBe(
      "https://github.com/owner/repo/commit/0123456789abcdef0123456789abcdef01234567",
    );
    wrapper.unmount();
  });

  it("copies the full hash through the shared copy helper", async () => {
    const wrapper = mountCard(deployment());
    await wrapper.find('[data-testid="commit-card"] button').trigger("click");
    expect(mockCopy).toHaveBeenCalledWith(
      "0123456789abcdef0123456789abcdef01234567",
      "Commit",
    );
    wrapper.unmount();
  });
});

describe("ApplicationLogsTab", () => {
  /** mountLogs renders the tab with stubbed log viewers. */
  function mountLogs(row: Deployment | null, overrides: Record<string, unknown> = {}) {
    return mount(ApplicationLogsTab, {
      props: {
        logServerId: "srv-1",
        logDeploymentId: row?.id ?? "",
        serverOptions: [{ label: "node-1", value: "srv-1" }],
        deploymentOptions: row
          ? [{ label: "deploy", value: row.id }]
          : [],
        activeDeploymentId: "",
        logTarget: row,
        effectiveLogServerId: "srv-1",
        application: {
          id: "app-1",
          repo: "owner/repo",
          clone_url: "https://github.com/owner/repo.git",
        } as never,
        runtimeDeployment: row,
        ...overrides,
      },
      global: {
        plugins: [i18n],
        stubs: { DeployLogs: true, LogViewer: true },
      },
    });
  }

  it("switches between deployment and runtime panes", async () => {
    const wrapper = mountLogs(deployment());
    expect(wrapper.text()).toContain("Deployment log");
    expect(wrapper.text()).toContain("Runtime log");
    expect(wrapper.html()).toContain("deploy-logs-stub");
    const tabs = wrapper.findAll(".n-tabs-tab");
    await tabs[1].trigger("click");
    expect(wrapper.html()).toContain("log-viewer-stub");
    wrapper.unmount();
  });

  it("unmounts the runtime viewer when switching back to deployment", async () => {
    const wrapper = mountLogs(deployment());
    const tabs = wrapper.findAll(".n-tabs-tab");
    await tabs[1].trigger("click");
    expect(wrapper.html()).toContain("log-viewer-stub");
    await tabs[0].trigger("click");
    expect(wrapper.html()).not.toContain("log-viewer-stub");
    expect(wrapper.html()).toContain("deploy-logs-stub");
    wrapper.unmount();
  });

  it("streams runtime from the application node with a hint on mismatch", async () => {
    const probe = {
      name: "LogViewer",
      props: ["serverId", "containerId"],
      template: '<div class="log-viewer-probe" :data-server="serverId"></div>',
    };
    const row = deployment();
    const wrapper = mount(ApplicationLogsTab, {
      props: {
        logServerId: "srv-other",
        logDeploymentId: row.id,
        serverOptions: [
          { label: "node-app", value: "srv-app" },
          { label: "node-other", value: "srv-other" },
        ],
        deploymentOptions: [{ label: "deploy", value: row.id }],
        activeDeploymentId: "",
        logTarget: row,
        effectiveLogServerId: "srv-other",
        application: {
          id: "app-1",
          repo: "owner/repo",
          clone_url: "",
          server_id: "srv-app",
        } as never,
        runtimeDeployment: row,
      },
      global: {
        plugins: [i18n],
        stubs: { DeployLogs: true, LogViewer: probe },
      },
    });
    const tabs = wrapper.findAll(".n-tabs-tab");
    await tabs[1].trigger("click");
    expect(wrapper.find(".log-viewer-probe").attributes("data-server")).toBe("srv-app");
    expect(wrapper.find('[data-testid="runtime-node-hint"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it("hides the node hint when the selection matches the application node", async () => {
    const wrapper = mountLogs(deployment(), {
      logServerId: "srv-1",
      effectiveLogServerId: "srv-1",
      application: {
        id: "app-1",
        repo: "owner/repo",
        clone_url: "",
        server_id: "srv-1",
      } as never,
    });
    const tabs = wrapper.findAll(".n-tabs-tab");
    await tabs[1].trigger("click");
    expect(wrapper.find('[data-testid="runtime-node-hint"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("shows an empty state when no container is running", async () => {
    const wrapper = mount(ApplicationLogsTab, {
      props: {
        logServerId: "",
        logDeploymentId: "",
        serverOptions: [],
        deploymentOptions: [],
        activeDeploymentId: "",
        logTarget: null,
        effectiveLogServerId: "",
        application: null,
        runtimeDeployment: null,
      },
      global: {
        plugins: [i18n],
        stubs: { DeployLogs: true, LogViewer: true },
      },
    });
    const tabs = wrapper.findAll(".n-tabs-tab");
    await tabs[1].trigger("click");
    expect(wrapper.text()).toContain("No running container");
    expect(wrapper.html()).not.toContain("log-viewer-stub");
    wrapper.unmount();
  });

  it("renders the new tab labels in Vietnamese", async () => {
    setLocale("vi", null);
    const wrapper = mountLogs(deployment());
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("Nhật ký triển khai");
    expect(wrapper.text()).toContain("Nhật ký runtime");
    wrapper.unmount();
  });
});
